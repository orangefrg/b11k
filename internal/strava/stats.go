package strava

import (
	"fmt"
	"math"
	"time"
)

func CalculateStats(activities ActivitySummaryList) {
	totalActivities := 0
	totalDistance := 0.0
	totalMovingTime := 0.0
	totalElapsedTime := 0.0
	totalElevationGain := 0.0
	totalCalories := 0.0
	totalSpeed := 0.0
	totalMaxSpeed := 0.0

	earliest := time.Time{}
	latest := time.Time{}

	for _, activity := range activities {
		activityTime, err := time.Parse(time.RFC3339, activity.StartDate)
		if err != nil {
			continue
		}
		if activityTime.Before(earliest) {
			earliest = activityTime
		}
		if activityTime.After(latest) {
			latest = activityTime
		}
		totalDistance += activity.Distance
		totalMovingTime += activity.MovingTime
		totalElapsedTime += activity.ElapsedTime
		totalElevationGain += activity.TotalElevationGain
		totalCalories += activity.Kilojoules * 0.239006
		totalSpeed += activity.AverageSpeed
		totalMaxSpeed += activity.MaxSpeed
		totalActivities++
	}
	avgDistancePerWeek := totalDistance / float64(latest.Sub(earliest).Hours()/24/7)
	avgCaloriesPerWeek := totalCalories / float64(latest.Sub(earliest).Hours()/24/7)
	avgElevationGainPerWeek := totalElevationGain / float64(latest.Sub(earliest).Hours()/24/7)
	avgDistancePerActivity := totalDistance / float64(totalActivities)
	avgCaloriesPerActivity := totalCalories / float64(totalActivities)
	avgElevationGainPerActivity := totalElevationGain / float64(totalActivities)
	avgIncline := totalElevationGain / totalDistance * 100
	avgInclineDegrees := math.Atan2(totalElevationGain, totalDistance) * 180 / math.Pi
	bikeTimePercentage := totalElapsedTime / latest.Sub(earliest).Seconds() * 100
	fmt.Printf("Total activities: %d\n", totalActivities)
	fmt.Printf("Total distance: %.0f km\n", totalDistance/1000)
	fmt.Printf("Total moving time: %.2f hours\n", totalMovingTime/3600)
	fmt.Printf("Total elapsed time: %.2f hours\n", totalElapsedTime/3600)
	fmt.Printf("Total elevation gain: %.0f m\n", totalElevationGain)
	fmt.Printf("Total calories: %.0f kcal\n", totalCalories)
	fmt.Printf("Avg distance: %.2f km per activity, %.2f km per week\n",
		avgDistancePerActivity/1000, avgDistancePerWeek/1000)
	fmt.Printf("Avg calories: %.2f kcal per activity, %.2f kcal per week\n",
		avgCaloriesPerActivity, avgCaloriesPerWeek)
	fmt.Printf("Avg elevation gain: %.2f m per activity, %.2f m per week\n",
		avgElevationGainPerActivity, avgElevationGainPerWeek)
	fmt.Printf("Virtual incline: %.2f%% (%.4f°)\n", avgIncline, avgInclineDegrees)
	fmt.Printf("Avg max speed: %.2f km/h, avg speed: %.2f km/h\n",
		totalMaxSpeed*3.6/(float64(totalActivities)), totalSpeed*3.6/float64(totalActivities))
	fmt.Printf("Bike time percentage: %.5f%%\n", bikeTimePercentage)
}
