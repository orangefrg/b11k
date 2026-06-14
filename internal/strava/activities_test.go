package strava

import (
	"testing"
	"time"
)

func TestDecodeRawStravaStreamsSupportsArrayAndKeyedResponses(t *testing.T) {
	arrayBody := []byte(`[
		{"type":"time","data":[0,1],"series_type":"time","original_size":2,"resolution":"high"},
		{"type":"altitude","data":[88.4,88.6],"series_type":"distance","original_size":2,"resolution":"high"}
	]`)
	arrayStreams, err := decodeRawStravaStreams(arrayBody)
	if err != nil {
		t.Fatalf("array decode failed: %v", err)
	}
	if len(arrayStreams) != 2 {
		t.Fatalf("array decode stream count = %d, want 2", len(arrayStreams))
	}
	if arrayStreams[0].Type != "time" || arrayStreams[1].Type != "altitude" {
		t.Fatalf("array stream types = %q, %q", arrayStreams[0].Type, arrayStreams[1].Type)
	}

	keyedBody := []byte(`{
		"time":{"data":[0,1],"series_type":"time","original_size":2,"resolution":"high"},
		"velocity_smooth":{"data":[4.1,4.2],"series_type":"distance","original_size":2,"resolution":"high"}
	}`)
	keyedStreams, err := decodeRawStravaStreams(keyedBody)
	if err != nil {
		t.Fatalf("keyed decode failed: %v", err)
	}
	types := map[string]bool{}
	for _, stream := range keyedStreams {
		types[stream.Type] = true
	}
	if !types["time"] || !types["velocity_smooth"] {
		t.Fatalf("keyed stream types missing expected keys: %v", types)
	}
}

func TestAddStreamsIncludesAllRequestedSensorStreams(t *testing.T) {
	start := time.Date(2026, 6, 1, 19, 13, 0, 0, time.UTC)
	activity := BikeActivity{
		Summary: ActivitySummary{
			ID:            123,
			StartDateTime: start,
		},
	}

	streams := []RawStravaStream{
		{Type: "time", Data: []interface{}{float64(0), float64(2)}},
		{Type: "latlng", Data: []interface{}{[]interface{}{44.8, 20.4}, []interface{}{44.81, 20.41}}},
		{Type: "distance", Data: []interface{}{0.0, 155.5}},
		{Type: "altitude", Data: []interface{}{88.4, 89.1}},
		{Type: "heartrate", Data: []interface{}{142.0, 143.0}},
		{Type: "velocity_smooth", Data: []interface{}{5.2, 5.4}},
		{Type: "watts", Data: []interface{}{210.0, 216.0}},
		{Type: "cadence", Data: []interface{}{82.0, 84.0}},
		{Type: "grade_smooth", Data: []interface{}{1.1, 1.4}},
		{Type: "moving", Data: []interface{}{true, true}},
		{Type: "temp", Data: []interface{}{24.0, 25.0}},
	}

	if err := activity.AddStreams(streams); err != nil {
		t.Fatalf("AddStreams failed: %v", err)
	}

	if len(activity.TimeStream.Data) != 2 || !activity.TimeStream.Data[1].Equal(start.Add(2*time.Second)) {
		t.Fatalf("time stream was not parsed correctly: %#v", activity.TimeStream.Data)
	}
	if len(activity.LatLngStream.Data) != 2 || activity.LatLngStream.Data[1][0] != 44.81 {
		t.Fatalf("latlng stream was not parsed correctly: %#v", activity.LatLngStream.Data)
	}
	if activity.DistanceStream.Data[1] != 155.5 {
		t.Fatalf("distance stream = %#v", activity.DistanceStream.Data)
	}
	if activity.HeartrateStream.Data[1] != 143 {
		t.Fatalf("heartrate stream = %#v", activity.HeartrateStream.Data)
	}
	if activity.SpeedStream.Data[1] != 5.4 {
		t.Fatalf("speed stream = %#v", activity.SpeedStream.Data)
	}
	if activity.WattsStream.Data[1] != 216 {
		t.Fatalf("watts stream = %#v", activity.WattsStream.Data)
	}
	if activity.CadenceStream.Data[1] != 84 {
		t.Fatalf("cadence stream = %#v", activity.CadenceStream.Data)
	}
	if activity.GradeStream.Data[1] != 1.4 {
		t.Fatalf("grade stream = %#v", activity.GradeStream.Data)
	}
	if !activity.MovingStream.Data[1] {
		t.Fatalf("moving stream = %#v", activity.MovingStream.Data)
	}
	if activity.TemperatureStream.Data[1] != 25 {
		t.Fatalf("temperature stream = %#v", activity.TemperatureStream.Data)
	}
}
