package main

import "fmt"

func clampFloat(v, min, max float32) float32 {
	if v < min { return min }
	if v > max { return max }
	return v
}

func softClampEdgePan(val, limit, zone float32) float32 {
	if limit < 0 { limit = -limit }
	if zone > limit { zone = limit }
	if zone <= 0 { return clampFloat(val, -limit, limit) }
	if val > limit - zone {
		if val >= limit + zone { return limit }
		x := val - (limit - zone)
		return limit - zone + x - (x*x)/(4*zone)
	}
	if val < -limit + zone {
		if val <= -limit - zone { return -limit }
		x := val - (-limit + zone)
		return -limit + zone + x + (x*x)/(4*zone)
	}
	return val
}

func main() {
	for v := float32(0); v <= 20; v++ {
		fmt.Printf("val: %.1f -> out: %.2f (diff: %.2f)\n", v, softClampEdgePan(v, 10, 5), softClampEdgePan(v, 10, 5)-softClampEdgePan(v-1, 10, 5))
	}
}
