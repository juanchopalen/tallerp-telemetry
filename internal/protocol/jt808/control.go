package jt808

// Terminal control command words (Table 15, subset).
const (
	ControlSetServer          byte = 0x02
	ControlShutdown           byte = 0x03
	ControlReset              byte = 0x04
	ControlFactoryReset       byte = 0x05
	ControlOilElectricCutoff  byte = 0x64
	ControlOilElectricRestore byte = 0x65
)

// BuildTerminalControl builds a 0x8105 terminal control message. This is a
// builder only — nothing in this codebase calls it automatically. Sending
// terminal control commands (shutdown, factory reset, oil/electricity
// cutoff, etc.) from TallERP is explicitly out of scope for this phase; see
// docs/jt808.md.
func BuildTerminalControl(terminalSN string, serial uint16, command byte, params string) ([]byte, error) {
	body := make([]byte, 0, 1+len(params))
	body = append(body, command)
	body = append(body, params...)
	return EncodeFrame(MsgTerminalControl, terminalSN, serial, body)
}
