package strava

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

/*
{
	"name" : "Happy Friday",
	"distance" : 24931.4,
	"moving_time" : 4500,
	"elapsed_time" : 4500,
	"total_elevation_gain" : 0,
	"type" : "Ride",
	"id" : 154504250376823,
	"start_date" : "2018-05-02T12:15:09Z",
	"utc_offset" : -25200,
	"start_latlng" : null,
	"end_latlng" : null,
	"location_city" : null,
	"location_state" : null,
	"location_country" : "United States",
	"gear_id" : "b12345678987654321",
	"average_speed" : 5.54,
	"max_speed" : 11,
	"average_cadence" : 67.1,
	"average_watts" : 210,
	"kilojoules" : 788.7,
	"average_heartrate" : 140.3,
	"max_heartrate" : 178,
	"max_watts" : 406,
	"suffer_score" : 82
  }
*/

type ActivitySummaryList []ActivitySummary
type BikeActivityList []BikeActivity

type ActivitySummary struct {
	ID                 int64      `json:"id"`
	AthleteID          int64      `json:"athlete_id"`
	Name               string     `json:"name"`
	Distance           float64    `json:"distance"`
	MovingTime         float64    `json:"moving_time"`
	ElapsedTime        float64    `json:"elapsed_time"`
	TotalElevationGain float64    `json:"total_elevation_gain"`
	Type               string     `json:"type"`
	SportType          string     `json:"sport_type"`
	WorkoutType        *int       `json:"workout_type"`
	StartDate          string     `json:"start_date"`
	UtcOffset          float64    `json:"utc_offset"`
	StartLatLng        *[]float64 `json:"start_latlng"`
	EndLatLng          *[]float64 `json:"end_latlng"`
	LocationCity       *string    `json:"location_city"`
	LocationState      *string    `json:"location_state"`
	LocationCountry    *string    `json:"location_country"`
	GearID             string     `json:"gear_id"`
	GearName           *string    `json:"gear_name,omitempty"`
	AverageSpeed       float64    `json:"average_speed"`
	MaxSpeed           float64    `json:"max_speed"`
	AverageCadence     float64    `json:"average_cadence"`
	AverageWatts       float64    `json:"average_watts"`
	Kilojoules         float64    `json:"kilojoules"`
	AverageHeartrate   float64    `json:"average_heartrate"`
	MaxHeartrate       float64    `json:"max_heartrate"`
	MaxWatts           float64    `json:"max_watts"`
	SufferScore        float64    `json:"suffer_score"`

	StartDateTime time.Time `json:"-"`
}

type BikeActivity struct {
	Summary ActivitySummary

	Gear *Gear `json:"gear"`

	Map struct {
		Polyline        string `json:"polyline"`
		SummaryPolyline string `json:"summary_polyline"`
	} `json:"map"`

	TimeStream        TimeStream
	LatLngStream      LatLngStream
	AltitudeStream    AltitudeStream
	HeartrateStream   HeartrateStream
	SpeedStream       SpeedStream
	WattsStream       WattsStream
	CadenceStream     CadenceStream
	GradeStream       GradeStream
	MovingStream      MovingStream
	DistanceStream    DistanceStream
	TemperatureStream TemperatureStream
}

type Gear struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TimeStream struct {
	Data []time.Time
}

type LatLngStream struct {
	Data [][]float64
}

type AltitudeStream struct {
	Data []float64
}

type HeartrateStream struct {
	Data []int
}

type SpeedStream struct {
	Data []float64
}

type WattsStream struct {
	Data []int
}

type CadenceStream struct {
	Data []int
}

type GradeStream struct {
	Data []float64
}

type MovingStream struct {
	Data []bool
}

type DistanceStream struct {
	Data []float64
}

type TemperatureStream struct {
	Data []int
}

type RawStravaStream struct {
	Type         string        `json:"type"`
	Data         []interface{} `json:"data"`
	Series       string        `json:"series_type"`
	OriginalSize int           `json:"original_size"`
	Resolution   string        `json:"resolution"`
}

var activityStreamKeys = []string{
	"time",
	"latlng",
	"distance",
	"altitude",
	"heartrate",
	"velocity_smooth",
	"watts",
	"cadence",
	"grade_smooth",
	"moving",
	"temp",
}

func (b *ActivitySummaryList) UnmarshalJSON(data []byte) error {
	var activities []ActivitySummary
	if err := json.Unmarshal(data, &activities); err != nil {
		return err
	}
	*b = activities
	return nil
}

func FetchBikeActivities(accessToken string, earliestTime time.Time, latestTime time.Time) (ActivitySummaryList, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var allActivities ActivitySummaryList
	page := 1
	perPage := 200

	fmt.Println("📄 Fetching activities page by page...")

	for {
		fmt.Printf("   Fetching page %d... ", page)

		url := fmt.Sprintf("https://www.strava.com/api/v3/athlete/activities?page=%d&per_page=%d", page, perPage)
		if !earliestTime.IsZero() {
			url += fmt.Sprintf("&after=%d", earliestTime.Unix())
		}
		if !latestTime.IsZero() {
			url += fmt.Sprintf("&before=%d", latestTime.Unix())
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to fetch activities with status %d: %s", resp.StatusCode, string(body))
		}

		var pageActivities ActivitySummaryList
		if err := json.Unmarshal(body, &pageActivities); err != nil {
			return nil, err
		}

		// If we get fewer activities than perPage, we've reached the last page
		if len(pageActivities) < perPage {
			allActivities = append(allActivities, pageActivities...)
			fmt.Printf("found %d activities (last page)\n", len(pageActivities))
			break
		}

		allActivities = append(allActivities, pageActivities...)
		fmt.Printf("found %d activities\n", len(pageActivities))

		page++

		// Small delay to respect API rate limits
		time.Sleep(100 * time.Millisecond)

		// Safety check to prevent infinite loops
		if page > 100 {
			fmt.Println("⚠️  Reached maximum page limit (100), stopping pagination")
			break
		}
	}

	fmt.Printf("📊 Total activities fetched: %d\n", len(allActivities))

	// Filter for biking activities
	var bikingActivities ActivitySummaryList
	for _, activity := range allActivities {
		if activity.Type == "Ride" {
			startDateTime, err := time.Parse(time.RFC3339, activity.StartDate)
			if err != nil {
				return nil, err
			}
			activity.StartDateTime = startDateTime
			bikingActivities = append(bikingActivities, activity)
		}
	}

	return bikingActivities, nil
}

func (a *ActivitySummary) ToString() string {
	sb := strings.Builder{}
	city := ""
	country := ""
	if a.LocationCity != nil {
		city = *a.LocationCity
	}
	if a.LocationCountry != nil {
		country = *a.LocationCountry
	}
	at := strings.TrimSpace(strings.Trim(fmt.Sprintf("%s, %s", city, country), ", "))
	sb.WriteString(fmt.Sprintf("%s (%s, %s, %.2f km for %02d:%02d)", a.Name, a.StartDateTime.Weekday(),
		a.StartDateTime.Format("2006-01-02 03:04"), a.Distance/1000.0, int(a.ElapsedTime/3600), int(a.ElapsedTime/60)%60))
	if at != "" {
		sb.WriteString(fmt.Sprintf(" at %s", at))
	}
	sb.WriteString(":\n")
	sb.WriteString(fmt.Sprintf("\t%.2f km/h, %.2f m, cad. %.0f,\n\t%.2f W, %.2f bpm, %.1f kcal\n",
		a.AverageSpeed*3.6, a.TotalElevationGain, a.AverageCadence,
		a.AverageWatts, a.AverageHeartrate, a.Kilojoules*0.239006))
	return sb.String()
}

func (a *ActivitySummaryList) ToString() string {
	sb := strings.Builder{}
	for _, activity := range *a {
		sb.WriteString(activity.ToString())
		sb.WriteString("\n")
	}
	return sb.String()
}

func (a *ActivitySummaryList) GetDetailedActivities(accessToken string) (BikeActivityList, error) {
	var detailedActivities BikeActivityList
	client := &http.Client{Timeout: 30 * time.Second}
	for _, activity := range *a {
		fmt.Printf("Fetching detailed activity %d (%s)...\n", activity.ID, activity.Name)
		activityURL := fmt.Sprintf("https://www.strava.com/api/v3/activities/%d", activity.ID)
		req, err := http.NewRequest("GET", activityURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to fetch activity with status %d: %s", resp.StatusCode, string(body))
		}
		var detailedActivity BikeActivity
		if err := json.Unmarshal(body, &detailedActivity); err != nil {
			return nil, fmt.Errorf("failed to unmarshal activity: %v", err)
		}
		detailedActivity.Summary = activity
		if detailedActivity.Gear != nil && detailedActivity.Gear.Name != "" {
			detailedActivity.Summary.GearName = &detailedActivity.Gear.Name
		}
		time.Sleep(100 * time.Millisecond)
		streamParams := url.Values{}
		streamParams.Set("keys", strings.Join(activityStreamKeys, ","))
		streamParams.Set("key_by_type", "true")
		streamUrl := fmt.Sprintf("https://www.strava.com/api/v3/activities/%d/streams?%s", activity.ID, streamParams.Encode())
		req, err = http.NewRequest("GET", streamUrl, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to do request: %v", err)
		}
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read body: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to fetch streams with status %d: %s", resp.StatusCode, string(body))
		}
		streams, err := decodeRawStravaStreams(body)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal streams: %v", err)
		}
		if err := detailedActivity.AddStreams(streams); err != nil {
			return nil, fmt.Errorf("failed to add streams: %v", err)
		}
		fmt.Print(detailedActivity.StreamsSummary())
		detailedActivities = append(detailedActivities, detailedActivity)

	}
	return detailedActivities, nil
}

func decodeRawStravaStreams(body []byte) ([]RawStravaStream, error) {
	var streams []RawStravaStream
	if err := json.Unmarshal(body, &streams); err == nil {
		return streams, nil
	}

	var keyedStreams map[string]RawStravaStream
	if err := json.Unmarshal(body, &keyedStreams); err != nil {
		return nil, err
	}

	streams = make([]RawStravaStream, 0, len(keyedStreams))
	for key, stream := range keyedStreams {
		if stream.Type == "" {
			stream.Type = key
		}
		streams = append(streams, stream)
	}
	return streams, nil
}

func (b *BikeActivity) AddStreams(streams []RawStravaStream) error {
	for _, stream := range streams {
		switch stream.Type {
		case "time":
			timeStream := TimeStream{
				Data: make([]time.Time, len(stream.Data)),
			}
			for i, data := range stream.Data {
				var curTime int64
				switch v := data.(type) {
				case int64:
					curTime = v
				case float64:
					curTime = int64(v)
				default:
					return fmt.Errorf("invalid time data: %v, type: %T, could not convert to int64", data, data)
				}
				timeStream.Data[i] = b.Summary.StartDateTime.Add(time.Duration(curTime) * time.Second)
			}
			b.TimeStream = timeStream
		case "latlng":
			latLngStream := LatLngStream{
				Data: make([][]float64, len(stream.Data)),
			}
			for i, data := range stream.Data {
				curLatLng, ok := data.([]interface{})
				if !ok {
					return fmt.Errorf("invalid latlng data: %v, type: %T, could not convert to []interface{}", data, data)
				}
				latLngStream.Data[i] = []float64{curLatLng[0].(float64), curLatLng[1].(float64)}
			}
			b.LatLngStream = latLngStream
		case "altitude":
			altitudeStream := AltitudeStream{
				Data: make([]float64, len(stream.Data)),
			}
			for i, data := range stream.Data {
				curAltitude, ok := data.(float64)
				if !ok {
					return fmt.Errorf("invalid altitude data: %v, type: %T, could not convert to float64", data, data)
				}
				altitudeStream.Data[i] = curAltitude
			}
			b.AltitudeStream = altitudeStream
		case "heartrate":
			heartrateStream := HeartrateStream{
				Data: make([]int, len(stream.Data)),
			}
			for i, data := range stream.Data {
				var curHeartrate int
				switch v := data.(type) {
				case int:
					curHeartrate = v
				case float64:
					curHeartrate = int(v)
				default:
					return fmt.Errorf("invalid heartrate data: %v, type: %T, could not convert to int", data, data)
				}
				heartrateStream.Data[i] = curHeartrate
			}
			b.HeartrateStream = heartrateStream
		case "velocity_smooth":
			speedStream := SpeedStream{
				Data: make([]float64, len(stream.Data)),
			}
			for i, data := range stream.Data {
				curSpeed, ok := data.(float64)
				if !ok {
					return fmt.Errorf("invalid speed data: %v, type: %T, could not convert to float64", data, data)
				}
				speedStream.Data[i] = curSpeed
			}
			b.SpeedStream = speedStream
		case "watts":
			wattsStream := WattsStream{
				Data: make([]int, len(stream.Data)),
			}
			for i, data := range stream.Data {
				var curWatts int
				switch v := data.(type) {
				case int:
					curWatts = v
				case float64:
					curWatts = int(v)
				default:
					return fmt.Errorf("invalid watts data: %v, type: %T, could not convert to int", data, data)
				}
				wattsStream.Data[i] = curWatts
			}
			b.WattsStream = wattsStream
		case "cadence":
			cadenceStream := CadenceStream{
				Data: make([]int, len(stream.Data)),
			}
			for i, data := range stream.Data {
				var curCadence int
				switch v := data.(type) {
				case int:
					curCadence = v
				case float64:
					curCadence = int(v)
				default:
					return fmt.Errorf("invalid cadence data: %v, type: %T, could not convert to int", data, data)
				}
				cadenceStream.Data[i] = curCadence
			}
			b.CadenceStream = cadenceStream
		case "grade_smooth":
			gradeStream := GradeStream{
				Data: make([]float64, len(stream.Data)),
			}
			for i, data := range stream.Data {
				curGrade, ok := data.(float64)
				if !ok {
					return fmt.Errorf("invalid grade data: %v, type: %T, could not convert to float64", data, data)
				}
				gradeStream.Data[i] = curGrade
			}
			b.GradeStream = gradeStream
		case "moving":
			movingStream := MovingStream{
				Data: make([]bool, len(stream.Data)),
			}
			for i, data := range stream.Data {
				curMoving, ok := data.(bool)
				if !ok {
					return fmt.Errorf("invalid moving data: %v, type: %T, could not convert to bool", data, data)
				}
				movingStream.Data[i] = curMoving
			}
			b.MovingStream = movingStream
		case "distance":
			distanceStream := DistanceStream{
				Data: make([]float64, len(stream.Data)),
			}
			for i, data := range stream.Data {
				curDistance, ok := data.(float64)
				if !ok {
					return fmt.Errorf("invalid distance data: %v, type: %T, could not convert to float64", data, data)
				}
				distanceStream.Data[i] = curDistance
			}
			b.DistanceStream = distanceStream
		case "temp":
			temperatureStream := TemperatureStream{
				Data: make([]int, len(stream.Data)),
			}
			for i, data := range stream.Data {
				var curTemperature int
				switch v := data.(type) {
				case int:
					curTemperature = v
				case float64:
					curTemperature = int(v)
				default:
					return fmt.Errorf("invalid temp data: %v, type: %T, could not convert to int", data, data)
				}
				temperatureStream.Data[i] = curTemperature
			}
			b.TemperatureStream = temperatureStream
		}
	}
	return nil
}

func (b *BikeActivity) StreamsSummary() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("   Streams for %d: ", b.Summary.ID))
	sb.WriteString(fmt.Sprintf("time=%d ", len(b.TimeStream.Data)))
	sb.WriteString(fmt.Sprintf("latlng=%d ", len(b.LatLngStream.Data)))
	sb.WriteString(fmt.Sprintf("distance=%d ", len(b.DistanceStream.Data)))
	sb.WriteString(fmt.Sprintf("altitude=%d ", len(b.AltitudeStream.Data)))
	sb.WriteString(fmt.Sprintf("heartrate=%d ", len(b.HeartrateStream.Data)))
	sb.WriteString(fmt.Sprintf("speed=%d ", len(b.SpeedStream.Data)))
	sb.WriteString(fmt.Sprintf("watts=%d ", len(b.WattsStream.Data)))
	sb.WriteString(fmt.Sprintf("cadence=%d ", len(b.CadenceStream.Data)))
	sb.WriteString(fmt.Sprintf("grade=%d ", len(b.GradeStream.Data)))
	sb.WriteString(fmt.Sprintf("moving=%d ", len(b.MovingStream.Data)))
	sb.WriteString(fmt.Sprintf("temp=%d\n", len(b.TemperatureStream.Data)))
	return sb.String()
}

func (b *BikeActivity) ToString() string {
	sb := strings.Builder{}
	sb.WriteString(b.Summary.ToString() + "\n")
	sb.WriteString(fmt.Sprintf("\tMap: %s\n", b.Map.Polyline))
	sb.WriteString("\tStreams stats:\n\t")
	sb.WriteString(b.StreamsSummary())
	return sb.String()
}
