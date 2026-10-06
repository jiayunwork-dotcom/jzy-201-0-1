package budget

import "math"

const speedOfLightKmPerS = 299792.458

// FreeSpaceLoss returns ITU-R P.530 free-space basic transmission loss in dB:
// L = 92.45 + 20log10(f_GHz) + 20log10(d_km).
func FreeSpaceLoss(frequencyMHz, distanceKm float64) float64 {
	fGHz := frequencyMHz / 1000
	return 92.45 + 20*math.Log10(fGHz) + 20*math.Log10(distanceKm)
}
