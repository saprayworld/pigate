package kernel

// round1 rounds to one decimal place for tidy JSON percentages / readings.
// Lives in an untagged file so both the platform-agnostic mock and the
// linux-only real implementation can share it.
func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10.0
}
