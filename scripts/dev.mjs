import { spawn } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const isWindows = process.platform === 'win32';

const tasks = [
  {
    name: 'web',
    command: 'pnpm',
    args: ['--filter', '@relay-api/web', 'dev'],
    cwd: rootDir,
  },
  {
    name: 'api',
    command: 'go',
    args: ['run', './cmd/server'],
    cwd: path.join(rootDir, 'server'),
  },
];

const children = new Map();
let shuttingDown = false;
let exitCode = 0;

function commandLabel(task) {
  return [task.command, ...task.args].join(' ');
}

function pipeWithPrefix(name, stream, output) {
  let pending = '';

  stream.on('data', (chunk) => {
    pending += chunk.toString();
    const lines = pending.split(/\r?\n/);
    pending = lines.pop() ?? '';

    for (const line of lines) {
      output.write(`[${name}] ${line}\n`);
    }
  });

  stream.on('end', () => {
    if (pending) {
      output.write(`[${name}] ${pending}\n`);
    }
  });
}

function stopChild(child) {
  if (!child.pid || child.exitCode !== null || child.signalCode !== null) {
    return;
  }

  if (isWindows) {
    const killer = spawn('taskkill', ['/PID', String(child.pid), '/T', '/F'], {
      stdio: 'ignore',
      windowsHide: true,
    });
    killer.on('error', () => child.kill());
    return;
  }

  child.kill('SIGTERM');
}

function shutdown(code = exitCode) {
  if (shuttingDown) {
    return;
  }

  shuttingDown = true;
  exitCode = code;

  for (const child of children.values()) {
    stopChild(child);
  }

  if (children.size === 0) {
    process.exit(exitCode);
  }

  setTimeout(() => process.exit(exitCode), 3000).unref();
}

function startTask(task) {
  console.log(`[${task.name}] $ ${commandLabel(task)}`);

  const child = spawn(task.command, task.args, {
    cwd: task.cwd,
    env: process.env,
    shell: isWindows,
    stdio: ['inherit', 'pipe', 'pipe'],
    windowsHide: true,
  });

  children.set(task.name, child);
  pipeWithPrefix(task.name, child.stdout, process.stdout);
  pipeWithPrefix(task.name, child.stderr, process.stderr);

  child.on('error', (error) => {
    console.error(`[${task.name}] failed to start: ${error.message}`);
    exitCode = 1;
    shutdown(1);
  });

  child.on('exit', (code, signal) => {
    children.delete(task.name);

    if (!shuttingDown) {
      const reason = signal ? `signal ${signal}` : `code ${code ?? 0}`;
      console.error(`[${task.name}] exited with ${reason}`);
      shutdown(code ?? 1);
      return;
    }

    if (children.size === 0) {
      process.exit(exitCode);
    }
  });
}

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    console.log(`received ${signal}, stopping dev processes...`);
    shutdown(0);
  });
}

for (const task of tasks) {
  startTask(task);
}
