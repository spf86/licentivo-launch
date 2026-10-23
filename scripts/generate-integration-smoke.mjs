// Exercise the exact connection code shown in Launch, without external traffic.
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const argumentsByName = Object.fromEntries(process.argv.slice(2).map((value, index, all) => index % 2 === 0 ? [value, all[index + 1]] : null).filter(Boolean));
const language = argumentsByName['--language'];
const output = path.resolve(argumentsByName['--output'] || '.cache/generated-integration-smoke');
const source = fs.readFileSync(path.join(root, 'internal/httpapi/web/integration-prompts.js'), 'utf8');
const {buildIntegrationPrompt, integrationTargets} = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
if (!Object.hasOwn(integrationTargets, language)) throw Error('Choose a supported integration language');
const product = '11111111-1111-4111-8111-111111111111';
const remote = '22222222-2222-4222-8222-222222222222';
// Quotes, backslashes, interpolation syntax and non-ASCII text must remain data.
const keys = {['key-\'"\\#{raise("unsafe")}-名🚀']: Buffer.alloc(32, 1).toString('base64url')};
const report = {ready:true, language, config:{schema_version:1,merchant_origin:'https://merchant.example',launch_product_id:product,licentivo_product_id:remote,environment:'sandbox'},product_config:{base_url:'https://www.licentivo.com',product_id:remote,trusted_keys:keys}};
const prompt = buildIntegrationPrompt({language, merchantOrigin:report.config.merchant_origin, environment:'sandbox', report});
if (!prompt.ready) throw Error('Fixture could not produce a ready connection');
fs.mkdirSync(output, {recursive:true});
function write(name, content) {fs.writeFileSync(path.join(output, name), content, {flag:'wx'});}
write(prompt.code.filename, prompt.code.content);
switch (language) {
  case 'go': case 'web': write('main.go', 'package main\nimport "fmt"\nfunc main(){app,err:=connectLaunch();if err!=nil{panic(err)};app.Close();fmt.Println("PASS generated Go direct initialization")}\n'); break;
  case 'python': write('main.py', 'from launch_connection import connect_launch\napp=connect_launch()\nassert app.origin == "https://merchant.example"\nassert app.config["base_url"] == "https://www.licentivo.com"\napp.close()\nprint("PASS generated Python direct initialization")\n'); break;
  case 'javascript': write('connect-smoke.test.mjs', "import test from 'node:test';\nimport {connectLaunch} from './launch-connection.mjs';\ntest('generated Node direct initialization',async()=>{const app=connectLaunch();await app.close();});\n"); break;
  case 'csharp':
    write('Program.cs', 'await using var app = LaunchConnection.Open();\nSystem.Console.WriteLine("PASS generated C# direct initialization");\n');
    write('Consumer.csproj', '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><ImplicitUsings>enable</ImplicitUsings><Nullable>enable</Nullable></PropertyGroup><ItemGroup><PackageReference Include="Licentivo.Launch" Version="0.1.2"/></ItemGroup></Project>\n'); break;
  case 'java': write('ConnectSmoke.java', 'public final class ConnectSmoke {public static void main(String[] args) throws Exception {try(var app=LaunchConnection.open()){System.out.println("PASS generated Java direct initialization");}}}\n'); break;
  case 'rust': write('main.rs', 'mod launch_connection;\nfn main(){let mut app=launch_connection::connect_launch().expect("connect");app.close();println!("PASS generated Rust direct initialization");}\n'); break;
  case 'ruby': write('main.rb', "require_relative 'launch_connection'\napp=connect_launch\napp.close\nputs 'PASS generated Ruby direct initialization'\n"); break;
  case 'c': write('main.c', '#include "launch_connection.c"\nint main(void){launch_application *app=connect_launch();if(!app)return 2;int state=launch_application_restore(app);launch_application_close(app);return state==0?0:3;}\n'); break;
  case 'cpp': write('main.cpp', '#include "launch_connection.cpp"\nint main(){auto app=connectLaunch();return app.Restore()?1:0;}\n'); break;
}
console.log(`Prepared the actual ${language} connection snippet; no public configuration files created.`);
