// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "github.com/apache/arrow-go/v18/arrow/array"

// Strip only the metrics decorator for internal state inspection. Tests still
// iterate, retain and release the original reader, exercising its accounting
// and ownership rather than replacing the public execution path.
func inspectionReader(reader array.RecordReader) array.RecordReader {
	for {
		wrapped, ok := reader.(*usageReader)
		if !ok {
			return reader
		}
		reader = wrapped.RecordReader
	}
}

// Integration fixtures may exercise enabled accounting, but must not publish
// synthetic workloads or create persistent installation identities. Explicit
// telemetry options supplied by a telemetry-specific test take precedence.
func testUsageOptions(options map[string]string) map[string]string {
	for key, value := range map[string]string{
		OptionStringTelemetryEndpoint:       "",
		OptionStringTelemetryInstallationID: "redis-adbc-integration-test",
		OptionStringTelemetryTrafficClass:   "ci",
	} {
		if key == OptionStringTelemetryInstallationID {
			if _, fileExplicit := options[OptionStringTelemetryInstallationIDFile]; fileExplicit {
				continue
			}
		}
		if _, exists := options[key]; !exists {
			options[key] = value
		}
	}
	return options
}
