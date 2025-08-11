import { customAlphabet } from 'nanoid'
export const rid = customAlphabet('0123456789abcdefghijklmnopqrstuvwxyz', 10)
export const uid = () => rid()
