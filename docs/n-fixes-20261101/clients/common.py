import http.cookiejar
import json
import os
from pathlib import Path
import time
import urllib.error
import urllib.request

BASE = Path('/home/spf86/n-fixes-20261101')
STATE = BASE / 'state-private.json'
EVIDENCE = BASE / 'evidence.jsonl'
SECRETS = json.loads((BASE / 'deployment-private.json').read_text())
L_ORIGIN = 'http://192.168.2.177:18092'
V_ORIGIN = 'http://127.0.0.1:18091'

def record(name, passed, **detail):
    row = {'time':time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'name':name, 'passed':bool(passed), **detail}
    with EVIDENCE.open('a') as stream:
        stream.write(json.dumps(row, ensure_ascii=False) + '\n')
    print(json.dumps(row), flush=True)
    return row

def state():
    return json.loads(STATE.read_text()) if STATE.exists() else {}

def save(value):
    temp = STATE.with_suffix('.tmp')
    temp.write_text(json.dumps(value, indent=2))
    temp.chmod(0o600)
    os.replace(temp, STATE)

class Client:
    def __init__(self, origin, token='', cookies=None):
        self.origin, self.token = origin, token
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.jar))
        for value in cookies or []:
            value = dict(value)
            value['rest'] = value.pop('_rest', {})
            self.jar.set_cookie(http.cookiejar.Cookie(**value))

    def saved(self):
        return {'origin':self.origin,'token':self.token,'cookies':[cookie.__dict__ for cookie in self.jar]}

    def call(self, method, path, body=None, headers=None, raw=None):
        h = {'Accept':'application/json','Content-Type':'application/json'}
        if not self.token and not (isinstance(body,dict) and body.get('kind')=='desktop'):
            h['Origin']=self.origin
        if self.token:
            h['Authorization'] = 'Bearer ' + self.token
        else:
            for cookie in self.jar:
                if cookie.name in ('launch_csrf','licenova_csrf'):
                    h['X-CSRF-Token'] = cookie.value
        h.update(headers or {})
        request = urllib.request.Request(self.origin + path, data=raw if raw is not None else (json.dumps(body).encode() if body is not None else None), headers=h, method=method)
        started = time.monotonic()
        try:
            response = self.opener.open(request, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        payload = response.read()
        try:
            payload = json.loads(payload)
        except ValueError:
            payload = {'raw_length':len(payload)}
        return response.status, payload, round(time.monotonic()-started, 4)

    def expect(self, name, method, path, body=None, status=200, code=None, required=True, **kwargs):
        actual, payload, seconds = self.call(method,path,body,**kwargs)
        error = payload.get('error',{}).get('code')
        ok = actual == status and (code is None or error == code)
        record(name, ok, status=actual, code=error, request_id=payload.get('request_id'), seconds=seconds)
        if required and not ok:
            # Only safe diagnostics; bodies can contain newly issued credentials.
            raise RuntimeError(name + ': HTTP ' + str(actual) + ' ' + str(error))
        return payload.get('data',payload)

def vadmin():
    c = Client(V_ORIGIN)
    c.expect('Licentivo CSRF', 'GET','/api/v1/auth/csrf')
    c.expect('Licentivo operator login','POST','/api/v1/auth/login',{'email':SECRETS['licentivo_admin_email'],'password':SECRETS['licentivo_admin_password']})
    return c

def ladmin():
    c=Client(L_ORIGIN)
    c.expect('Launch operator login','POST','/api/v1/auth/login',{'email':SECRETS['launch_admin_email'],'password':SECRETS['launch_admin_password'],'kind':'website'})
    return c
