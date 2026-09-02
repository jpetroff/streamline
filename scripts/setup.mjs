import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

const manifest = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));
const goMod = readFileSync(new URL('../go.mod', import.meta.url), 'utf8');
const minimumGo = goMod.match(/^go (\S+)$/m)[1];
const go = process.argv[2] ?? 'go';

if (!Bun.semver.satisfies(Bun.version, manifest.engines.bun)) {
  throw new Error(`Use a globally installed Bun ${manifest.engines.bun}; found ${Bun.version}.`);
}

let goVersion;
try {
  goVersion = execFileSync(go, ['env', 'GOVERSION'], {
    encoding: 'utf8',
    env: { ...process.env, GOTOOLCHAIN: 'local' },
    stdio: ['ignore', 'pipe', 'ignore'],
  }).trim().replace(/^go/, '');
} catch {
  throw new Error('Go is unavailable. Install Go with Linuxbrew (brew install go) and check HOMEBREW_PREFIX/PATH.');
}

if (!Bun.semver.satisfies(goVersion, `>=${minimumGo}`)) {
  throw new Error(`Use Go ${minimumGo} or newer from Linuxbrew; found ${goVersion}. Run brew upgrade go.`);
}

console.log(`Global tools ready: Go ${goVersion}, Bun ${Bun.version}`);
