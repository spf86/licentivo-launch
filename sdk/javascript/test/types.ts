import {Application, MemoryStore, type SessionState} from '@licentivo/launch';

const app: Application = Application.connect(
  'https://merchant.example',
  '11111111-1111-4111-8111-111111111111',
  '22222222-2222-4222-8222-222222222222',
  {'test-key': 'AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE'},
  {environment: 'sandbox', store: new MemoryStore()},
);

const signedIn: Promise<SessionState> = app.login('user@example.test', 'test-password');
const result: Promise<number> = app.runFeature('export', () => 42);
const restored: Promise<SessionState | null> = app.restore();
const signedOut: Promise<boolean> = app.logout();
const released: Promise<void> = app.releaseDevice();
const closed: Promise<void> = app.close();
void [signedIn, result, restored, signedOut, released, closed];
