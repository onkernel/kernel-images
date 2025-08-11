export const b64 = (buf) => Buffer.from(buf).toString('base64')
export const b64json = (obj) => b64(Buffer.from(JSON.stringify(obj)))
export const fromB64 = (s) => Buffer.from(s, 'base64')
