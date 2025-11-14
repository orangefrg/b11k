package pggeo

import (
	"context"
	"fmt"
	"log"
	"time"

	"b11k/internal/strava"
)

// ExampleUsage demonstrates how to use the pggeo package
func ExampleUsage() {
	// Connect to database
	ctx := context.Background()
	conn, err := Connect(ctx, "user", "password", "localhost", "5432", "b11k")
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer conn.Close(ctx)

	// Create tables if they don't exist
	if err := CreateTables(ctx, conn); err != nil {
		log.Fatal("Failed to create tables:", err)
	}
	fmt.Println("✅ Tables created successfully")

	// Example: Insert a sample activity
	sampleActivity := createSampleActivity()
	if err := InsertBikeActivity(ctx, conn, sampleActivity); err != nil {
		log.Fatal("Failed to insert activity:", err)
	}
	fmt.Println("✅ Sample activity inserted successfully")

	// Example: Try to insert the same activity again (should fail)
	if err := InsertBikeActivity(ctx, conn, sampleActivity); err != nil {
		fmt.Printf("✅ Duplicate prevention working: %v\n", err)
	} else {
		fmt.Println("❌ Duplicate prevention failed!")
	}

	// Example: Check if activity exists
	exists, err := ActivityExists(ctx, conn, sampleActivity.Summary.ID)
	if err != nil {
		log.Fatal("Failed to check activity existence:", err)
	}
	fmt.Printf("✅ Activity %d exists: %t\n", sampleActivity.Summary.ID, exists)

	// Example: Check multiple activities at once
	activityIDs := []int64{sampleActivity.Summary.ID, 999999999, 888888888}
	existsMap, err := ActivitiesExist(ctx, conn, activityIDs)
	if err != nil {
		log.Fatal("Failed to check multiple activities:", err)
	}
	fmt.Printf("✅ Activity existence check: %+v\n", existsMap)

	// Example: Upsert activity (allows overwriting)
	sampleActivity.Summary.Name = "Updated Sample Bike Ride"
	if err := InsertBikeActivityUpsert(ctx, conn, sampleActivity); err != nil {
		log.Fatal("Failed to upsert activity:", err)
	}
	fmt.Println("✅ Activity upserted successfully")

	// Example: Query activities by date range
	startDate := time.Now().AddDate(0, -1, 0) // 1 month ago
	endDate := time.Now()
	// Example athlete ID - replace with actual athlete ID in real usage
	var athleteID int64 = 123456789
	activities, err := GetActivitiesByDateRange(ctx, conn, athleteID, startDate, endDate)
	if err != nil {
		log.Fatal("Failed to query activities:", err)
	}
	fmt.Printf("✅ Found %d activities in date range\n", len(activities))

	// Example: Query activities in a bounding box (example: San Francisco area)
	activities, err = GetActivitiesInBoundingBox(ctx, conn, 37.7, -122.5, 37.8, -122.4)
	if err != nil {
		log.Fatal("Failed to query activities in bounding box:", err)
	}
	fmt.Printf("✅ Found %d activities in bounding box\n", len(activities))

	// Example: Find activities near a specific point
	nearResults, err := FindActivitiesNear(ctx, conn, -122.4194, 37.7749, 1000) // 1km radius
	if err != nil {
		log.Fatal("Failed to find activities near point:", err)
	}
	fmt.Printf("✅ Found %d activities within 1km of point\n", len(nearResults))

	// Example: Find activities intersecting a line (example route)
	lineWKT := "LINESTRING(-122.4194 37.7749, -122.4094 37.7849)"
	intersectionResults, err := FindActivitiesIntersectingLine(ctx, conn, lineWKT, 50) // 50m tolerance
	if err != nil {
		log.Fatal("Failed to find activities intersecting line:", err)
	}
	fmt.Printf("✅ Found %d activities intersecting the line\n", len(intersectionResults))

	// Example: Refresh simplified geometries for all activities
	if err := RefreshAllSimplified(ctx, conn, 10.0); err != nil {
		log.Fatal("Failed to refresh simplified geometries:", err)
	}
	fmt.Println("✅ Refreshed simplified geometries for all activities")

	// Example: Get point samples for an activity
	if len(activities) > 0 {
		samples, err := GetPointSamplesForActivity(ctx, conn, athleteID, activities[0].ID)
		if err != nil {
			log.Fatal("Failed to get point samples:", err)
		}
		fmt.Printf("✅ Found %d point samples for activity %d\n", len(samples), activities[0].ID)
	}

	// Example: Create a favorite segment
	segmentPoints := [][]float64{
		{37.7749, -122.4194}, // San Francisco
		{37.7849, -122.4094}, // Slightly northeast
		{37.7949, -122.3994}, // Further northeast
	}
	// Example athlete ID (in real usage, get from authenticated user)
	exampleAthleteID := int64(12345)
	segment, err := InsertFavoriteSegment(ctx, conn, exampleAthleteID, "Golden Gate Segment", "A test segment in San Francisco", segmentPoints, nil)
	if err != nil {
		log.Fatal("Failed to create favorite segment:", err)
	}
	fmt.Printf("✅ Created favorite segment: %s (ID: %d)\n", segment.Name, segment.ID)

	// Example: Find route parts matching the segment
	matches, err := FindRoutePartsMatchingSegment(ctx, conn, segment.ID, 50) // 50m tolerance
	if err != nil {
		log.Fatal("Failed to find matching route parts:", err)
	}
	fmt.Printf("✅ Found %d route parts matching segment '%s'\n", len(matches), segment.Name)

	// Example: List all favorite segments for an athlete
	segments, err := ListFavoriteSegments(ctx, conn, exampleAthleteID)
	if err != nil {
		log.Fatal("Failed to list favorite segments:", err)
	}
	fmt.Printf("✅ Found %d favorite segments\n", len(segments))

	// Example: Find route parts matching segment by name
	matchesByName, err := FindRoutePartsMatchingSegmentByName(ctx, conn, "Golden Gate Segment", 100) // 100m tolerance
	if err != nil {
		log.Fatal("Failed to find matching route parts by name:", err)
	}
	fmt.Printf("✅ Found %d route parts matching segment by name\n", len(matchesByName))
}

// createSampleActivity creates a sample bike activity for testing
func createSampleActivity() *strava.BikeActivity {
	now := time.Now()

	activity := &strava.BikeActivity{
		Summary: strava.ActivitySummary{
			ID:                 123456789,
			Name:               "Sample Bike Ride",
			Distance:           15000, // 15km
			MovingTime:         3600,  // 1 hour
			ElapsedTime:        3600,
			TotalElevationGain: 200,
			Type:               "Ride",
			SportType:          "MountainBikeRide",
			StartDate:          now.Format(time.RFC3339),
			StartDateTime:      now,
			UtcOffset:          0,
			LocationCountry:    &[]string{"United States"}[0],
			LocationCity:       &[]string{"San Francisco"}[0],
			LocationState:      &[]string{"California"}[0],
			GearID:             "b123456789",
			AverageSpeed:       4.17, // ~15 km/h
			MaxSpeed:           8.33, // ~30 km/h
			AverageCadence:     80,
			AverageWatts:       200,
			Kilojoules:         720,
			AverageHeartrate:   150,
			MaxHeartrate:       180,
			MaxWatts:           400,
			SufferScore:        50,
		},
		Map: struct {
			Polyline        string `json:"polyline"`
			SummaryPolyline string `json:"summary_polyline"`
		}{
			Polyline:        "sample_polyline_data",
			SummaryPolyline: "sample_summary_polyline",
		},
	}

	// Create sample stream data
	numPoints := 100
	activity.TimeStream = strava.TimeStream{
		Data: make([]time.Time, numPoints),
	}
	activity.LatLngStream = strava.LatLngStream{
		Data: make([][]float64, numPoints),
	}
	activity.AltitudeStream = strava.AltitudeStream{
		Data: make([]float64, numPoints),
	}
	activity.HeartrateStream = strava.HeartrateStream{
		Data: make([]int, numPoints),
	}
	activity.SpeedStream = strava.SpeedStream{
		Data: make([]float64, numPoints),
	}
	activity.WattsStream = strava.WattsStream{
		Data: make([]int, numPoints),
	}
	activity.CadenceStream = strava.CadenceStream{
		Data: make([]int, numPoints),
	}
	activity.GradeStream = strava.GradeStream{
		Data: make([]float64, numPoints),
	}
	activity.MovingStream = strava.MovingStream{
		Data: make([]bool, numPoints),
	}

	// Fill with sample data
	baseLat := 37.7749
	baseLng := -122.4194
	for i := 0; i < numPoints; i++ {
		activity.TimeStream.Data[i] = now.Add(time.Duration(i) * time.Second)
		activity.LatLngStream.Data[i] = []float64{
			baseLat + float64(i)*0.0001, // Move north
			baseLng + float64(i)*0.0001, // Move east
		}
		activity.AltitudeStream.Data[i] = 100 + float64(i)*2 // Climbing
		activity.HeartrateStream.Data[i] = 140 + i%40        // Varying HR
		activity.SpeedStream.Data[i] = 3 + float64(i%20)*0.2 // Varying speed
		activity.WattsStream.Data[i] = 180 + i%60            // Varying power
		activity.CadenceStream.Data[i] = 75 + i%20           // Varying cadence
		activity.GradeStream.Data[i] = float64(i%10 - 5)     // Varying grade
		activity.MovingStream.Data[i] = i%10 != 0            // Mostly moving
	}

	return activity
}
