package probe

import (
	"context"
	"errors"
	"testing"
	"time"

	"ecs/internal/failure"
	"ecs/internal/model"
)

func TestOoklaContextCausePrecedesExecuteAndParseClassification(t *testing.T) {
	missingPath := t.TempDir() + "/missing-speedtest"
	cancelCause := errors.New("fixture Ookla cancellation cause")
	cancelled, cancel := context.WithCancelCause(context.Background())
	cancel(cancelCause)
	_, runErr, parseErr, contextDone := runOfficialOokla(cancelled, missingPath, nil)
	if !contextDone || parseErr != nil || !errors.Is(runErr, cancelCause) || !errors.Is(runErr, context.Canceled) {
		t.Fatalf("cancelled Ookla execution/parse = context_done:%v run:%v parse:%v", contextDone, runErr, parseErr)
	}
	if classified := failure.Classify(runErr); classified.Category != model.FailureCanceled {
		t.Fatalf("cancelled Ookla category = %+v", classified)
	}

	deadline, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	_, runErr, parseErr, contextDone = runOfficialOokla(deadline, missingPath, nil)
	if !contextDone || parseErr != nil || !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("deadline Ookla execution/parse = context_done:%v run:%v parse:%v", contextDone, runErr, parseErr)
	}
	if classified := failure.Classify(runErr); classified.Category != model.FailureTimeout {
		t.Fatalf("deadline Ookla category = %+v", classified)
	}

	path := writeThroughputExecutable(t, "speedtest", "#!/bin/sh\nprintf '%s' '{bad}'\n")
	_, runErr, parseErr, contextDone = runOfficialOokla(context.Background(), path, nil)
	if contextDone || runErr != nil || parseErr == nil {
		t.Fatalf("parse Ookla execution/parse = context_done:%v run:%v parse:%v", contextDone, runErr, parseErr)
	}
	if classified := failure.Classify(parseErr); classified.Category != model.FailureParse {
		t.Fatalf("parse Ookla category = %+v", classified)
	}
}

func TestOoklaJSONFixturesAndMeasurements(t *testing.T) {
	full, err := parseOoklaJSON([]byte(`{
		"ping":{"jitter":1.5,"latency":8.5},
		"download":{"bandwidth":125000000},
		"upload":{"bandwidth":25000000},
		"packetLoss":0,
		"isp":"Fixture ISP",
		"interface":{"externalIp":"203.0.113.9"},
		"server":{"id":42,"name":"Example","location":"London","country":"GB"}
	}`))
	if err != nil || full.ISP != "Fixture ISP" || full.Interface.ExternalIP != "203.0.113.9" || formatOoklaServer(full) != "Example · London · GB" {
		t.Fatalf("Ookla parsed metadata = %+v, err=%v", full, err)
	}
	result := model.NewResult("ookla", "Ookla")
	appendOoklaMeasurementsFor(&result, full, "")
	values := make(map[string]model.Measurement, len(result.Measurements))
	for _, measurement := range result.Measurements {
		values[measurement.Key] = measurement
	}
	if len(values) != 5 || values["ookla_ping_ms"].Value != 8.5 || values["ookla_jitter_ms"].Value != 1.5 || values["ookla_download_mbps"].Value != 1000 || values["ookla_upload_mbps"].Value != 200 || values["ookla_packet_loss_percent"].Value != 0 {
		t.Fatalf("Ookla measurements = %+v", values)
	}
	if values["ookla_ping_ms"].Unit != "ms" || values["ookla_download_mbps"].Unit != "Mbps" || values["ookla_packet_loss_percent"].Unit != "%" {
		t.Fatalf("Ookla measurement units = %+v", values)
	}
	if values["ookla_ping_ms"].HigherIsBetter == nil || *values["ookla_ping_ms"].HigherIsBetter ||
		values["ookla_jitter_ms"].HigherIsBetter == nil || *values["ookla_jitter_ms"].HigherIsBetter ||
		values["ookla_packet_loss_percent"].HigherIsBetter == nil || *values["ookla_packet_loss_percent"].HigherIsBetter ||
		values["ookla_download_mbps"].HigherIsBetter == nil || !*values["ookla_download_mbps"].HigherIsBetter ||
		values["ookla_upload_mbps"].HigherIsBetter == nil || !*values["ookla_upload_mbps"].HigherIsBetter {
		t.Fatalf("Ookla measurement directions = %+v", values)
	}
	for _, test := range []struct {
		alias, want string
	}{
		{alias: "电信", want: "telecom"}, {alias: "telecom", want: "telecom"}, {alias: "ct", want: "telecom"}, {alias: "chinatelecom", want: "telecom"},
		{alias: "联通", want: "unicom"}, {alias: "unicom", want: "unicom"}, {alias: "cu", want: "unicom"}, {alias: "chinaunicom", want: "unicom"},
		{alias: "移动", want: "mobile"}, {alias: "mobile", want: "mobile"}, {alias: "cm", want: "mobile"}, {alias: "chinamobile", want: "mobile"},
	} {
		if got := ooklaCarrierKey(test.alias); got != test.want {
			t.Errorf("Ookla carrier key for %q = %q, want %q", test.alias, got, test.want)
		}
	}
	if key, ok := ooklaCarrierValue("auto").Key(); !ok || key != "probe.ookla.carrier.auto" {
		t.Fatalf("Ookla automatic carrier value = %#v", ooklaCarrierValue("auto"))
	}

	partial, err := parseOoklaJSON([]byte(`{"ping":{"latency":8.5}}`))
	if err != nil || !ooklaHasValidMetric(partial) || ooklaMeasurementsComplete(partial) {
		t.Fatalf("partial Ookla result = %+v, err=%v", partial, err)
	}
	partialResult := model.NewResult("ookla", "Ookla")
	appendOoklaMeasurementsFor(&partialResult, partial, "")
	if len(partialResult.Measurements) != 1 {
		t.Fatalf("partial Ookla measurements = %+v", partialResult.Measurements)
	}
	empty, err := parseOoklaJSON([]byte(`{}`))
	if err != nil || ooklaHasValidMetric(empty) {
		t.Fatalf("empty Ookla result = %+v, err=%v", empty, err)
	}
	for _, raw := range []string{"{bad}", "[]"} {
		if _, err := parseOoklaJSON([]byte(raw)); err == nil {
			t.Fatalf("invalid Ookla JSON %q was accepted", raw)
		}
	}
}
