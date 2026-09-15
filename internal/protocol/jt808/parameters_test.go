package jt808

import "testing"

func TestBuildAndParseSetParametersRoundTripsViaQueryResponse(t *testing.T) {
	t.Parallel()
	params := map[uint32][]byte{
		ParamServerTCPPort:         EncodeDWORDParam(8899),
		ParamWorkingReportInterval: EncodeDWORDParam(60),
		ParamMainServerAddress:     EncodeStringParam("telemetry.tallerp.com"),
	}
	setFrame, err := BuildSetParameters("013800100034", 1, params)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleFrame(t, setFrame)
	if decoded.MessageID != MsgSetTerminalParameters {
		t.Fatalf("MessageID = 0x%04x, want 0x%04x", decoded.MessageID, MsgSetTerminalParameters)
	}
	if decoded.Body[0] != byte(len(params)) {
		t.Fatalf("declared param count = %d, want %d", decoded.Body[0], len(params))
	}

	// Build a synthetic 0x0104 response body using the same set-params
	// wire shape (minus the leading count byte's position, which the
	// query response prefixes with a response serial instead).
	body := append([]byte{0x00, 0x01}, decoded.Body...)
	response, err := ParseQueryParametersResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if response.ResponseSerial != 1 {
		t.Fatalf("ResponseSerial = %d, want 1", response.ResponseSerial)
	}
	if len(response.Params) != len(params) {
		t.Fatalf("got %d params, want %d", len(response.Params), len(params))
	}
	found := map[uint32][]byte{}
	for _, item := range response.Params {
		found[item.ID] = item.Value
	}
	if string(found[ParamMainServerAddress]) != "telemetry.tallerp.com" {
		t.Fatalf("ParamMainServerAddress = %q, want telemetry.tallerp.com", found[ParamMainServerAddress])
	}
}

func TestBuildQueryParametersHasEmptyBody(t *testing.T) {
	t.Parallel()
	frame, err := BuildQueryParameters("013800100034", 1)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleFrame(t, frame)
	if len(decoded.Body) != 0 {
		t.Fatalf("expected empty body, got %d bytes", len(decoded.Body))
	}
}

func TestParseQueryParametersResponseRejectsTruncated(t *testing.T) {
	t.Parallel()
	if _, err := ParseQueryParametersResponse([]byte{0x00}); err == nil {
		t.Fatal("expected an error for a too-short response body")
	}
	if _, err := ParseQueryParametersResponse([]byte{0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x13, 0xff}); err == nil {
		t.Fatal("expected an error for a parameter declaring length beyond the body")
	}
}
