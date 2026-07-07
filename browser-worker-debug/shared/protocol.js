export const MessageType = {
  // Server -> Extension
  JOB_REQUEST: 'job_request',
  PING: 'ping',
  CANCEL_JOB: 'cancel_job',
  REGISTER_RESPONSE: 'register_response',

  // Extension -> Server
  JOB_RESULT: 'job_result',
  PONG: 'pong',
  HEARTBEAT: 'heartbeat',
  HEARTBEAT_ACK: 'heartbeat_ack',
  REGISTER: 'register',

  // Internal
  STATUS_UPDATE: 'status_update',
};

export const JobStatus = {
  OK: 'ok',
  ERROR: 'error',
  CHALLENGE: 'challenge',
  WAITING_USER: 'waiting_user',
};

export const WorkerState = {
  DISCONNECTED: 'disconnected',
  CONNECTING: 'connecting',
  CONNECTED: 'connected',
  IDLE: 'idle',
  DOWNLOADING: 'downloading',
  RECOVERING: 'recovering',
  UNAUTHENTICATED: 'unauthenticated',
};

export function createMessage(type, payload = {}) {
  return JSON.stringify({ type, payload, timestamp: Date.now() });
}

export function parseMessage(data) {
  try {
    const msg = typeof data === 'string' ? JSON.parse(data) : data;
    return { type: msg.type, payload: msg.payload || {}, timestamp: msg.timestamp };
  } catch {
    return null;
  }
}
