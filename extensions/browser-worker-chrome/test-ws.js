#!/usr/bin/env node

const WebSocket = require('ws');

const SERVER_URL = process.argv[2] || 'ws://localhost:5176/ws/browser-worker';

console.log('Testing WebSocket connection to:', SERVER_URL);

const ws = new WebSocket(SERVER_URL);

ws.on('open', () => {
  console.log('Connected!');
  
  // Send registration
  ws.send(JSON.stringify({
    type: 'register',
    payload: {
      browser: { name: 'test', userAgent: 'test-agent' },
      capabilities: ['cookies', 'dom', 'javascript', 'websocket'],
      version: '1.0.0',
    },
    timestamp: Date.now(),
  }));
  
  console.log('Sent registration');
  
  // Send a test job after 2 seconds
  setTimeout(() => {
    const testJob = {
      type: 'job_request',
      payload: {
        jobId: 'test-001',
        operation: 'get_novel_info',
        url: 'https://www.69shuba.com/book/59083.htm',
        params: {},
      },
      timestamp: Date.now(),
    };
    console.log('Sending test job:', testJob);
    ws.send(JSON.stringify(testJob));
  }, 2000);
});

ws.on('message', (data) => {
  console.log('Received:', JSON.parse(data));
});

ws.on('error', (err) => {
  console.error('WebSocket error:', err.message);
});

ws.on('close', (code, reason) => {
  console.log('Connection closed:', code, reason.toString());
  process.exit(0);
});

// Timeout after 30 seconds
setTimeout(() => {
  console.log('Test timeout - closing connection');
  ws.close();
  process.exit(1);
}, 30000);
