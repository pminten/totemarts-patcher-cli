package patcher

// An Averager computes a running average of measurements.
type Averager struct {
	totalMeasurements int
	measurements      []float64
}

// NewAverager creates an averager with a particular window.
func NewAverager(window int) *Averager {
	return &Averager{
		totalMeasurements: 0,
		measurements:      make([]float64, window),
	}
}

// Add adds a measurement to the averager, overwriting the oldest measurement if
// there are more measurements than the window.
func (a *Averager) Add(measurement float64) {
	a.measurements[a.totalMeasurements%len(a.measurements)] = measurement
	a.totalMeasurements++
}

// Average computes the current running average. Returns 0 if there are no measurements.
func (a *Averager) Average() float64 {
	sum := 0.0
	count := min(a.totalMeasurements, len(a.measurements))
	if count == 0 {
		return 0 // Avoid div by zero so upstream doesn't need to deal with NaN.
	}
	for _, m := range a.measurements[:count] {
		sum += m
	}
	return sum / float64(count)
}
