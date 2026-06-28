package main

import "fmt"

func clampFloat(v, min, max float32) float32 {
	if v < min { return min }
	if v > max { return max }
	return v
}

func softClamp(val, min, max, zone float32) float32 {
	if min >= max {
		return min
	}
	center := (max + min) / 2
	limit := (max - min) / 2
	valC := val - center
	
	if zone > limit {
		zone = limit
	}
	if zone <= 0 {
		return clampFloat(valC, -limit, limit) + center
	}
	if valC > limit-zone {
		if valC >= limit+zone {
			return max
		}
		x := valC - (limit - zone)
		return limit - zone + x - (x*x)/(4*zone) + center
	}
	if valC < -limit+zone {
		if valC <= -limit-zone {
			return min
		}
		x := valC - (-limit + zone)
		return -limit + zone + x + (x*x)/(4*zone) + center
	}
	return valC + center
}

func main() {
	for v := float32(-20); v <= 20; v++ {
		fmt.Printf("val: %5.1f -> out: %5.2f (diff: %5.2f)\n", v, softClamp(v, -10, 10, 5), softClamp(v, -10, 10, 5)-softClamp(v-1, -10, 10, 5))
	}
}
