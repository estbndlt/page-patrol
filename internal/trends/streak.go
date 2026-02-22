package trends

func ComputeStreak(values []bool) int {
	streak := 0
	for _, value := range values {
		if !value {
			break
		}
		streak++
	}
	return streak
}
