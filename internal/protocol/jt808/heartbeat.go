package jt808

// Heartbeat (0x0002) carries an empty body; nothing to parse beyond the
// header. Kept as a named predicate for readability at call sites.
func IsHeartbeat(frame Frame) bool { return frame.MessageID == MsgTerminalHeartbeat }
