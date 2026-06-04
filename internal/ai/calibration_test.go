package ai

import "testing"

func TestParseCalibrationResult_OK(t *testing.T) {
	ok, revised := parseCalibrationResult(`{"ok":true,"reason":"good","revised_reply":""}`)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if revised != "" {
		t.Fatalf("expected empty revised reply, got %q", revised)
	}
}

func TestParseCalibrationResult_UsesRevisedReply(t *testing.T) {
	ok, revised := parseCalibrationResult(`{"ok":false,"reason":"too generic","revised_reply":"I choose 67."}`)
	if ok {
		t.Fatal("expected ok=false")
	}
	if revised != "I choose 67." {
		t.Fatalf("unexpected revised reply: %q", revised)
	}
}

func TestParseCalibrationResult_GracefulOnInvalidJSON(t *testing.T) {
	ok, revised := parseCalibrationResult(`not json`)
	if !ok || revised != "" {
		t.Fatalf("expected graceful fallback, got ok=%v revised=%q", ok, revised)
	}
}
