package service

// This file contains a utility to verify Google Drive cache readiness
//
// Problem: io.Copy(io.Discard, file) completes BEFORE Android fully caches the file.
// Checking only ReadAt is insufficient - ReadAt might work while the file is still downloading.
//
// Solution: Comprehensive cache readiness check:
// 1. Check that the file size is stable (not growing) - means download is complete
// 2. Check that ReadAt is fast (>10 MB/s) - means we are reading from local cache
// 3. Require multiple consecutive checks with stable size

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// WaitForGDriveCache waits until Google Drive file is fully cached by Android
//
// Parameters:
// - file: opened file via SAF
// - expectedSize: expected file size (from initial Stat)
// - maxWaitTime: maximum wait time
//
// Returns nil if cache is ready, error if timeout or other error
func WaitForGDriveCache(file *os.File, expectedSize int64, maxWaitTime time.Duration) error {
	logrus.Infof("📍 [GDRIVE-CACHE-WAIT] Starting to wait for file caching (expected size: %d bytes, max time: %v)", expectedSize, maxWaitTime)

	testBuf := make([]byte, 65536) // 64KB buffer for speed test
	interval := 200 * time.Millisecond
	maxAttempts := int(maxWaitTime / interval)

	// Verification parameters
	previousSize := expectedSize
	stableSizeCount := 0
	const requiredStableChecks = 3 // Requires 3 checks with same size in a row
	const minReadSpeedMBps = 10.0  // Minimum speed for local cache (MB/s)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Step 1: Check current file size
		currentStat, statErr := file.Stat()
		if statErr != nil {
			logrus.Warnf("⚠️ [GDRIVE-CACHE-WAIT] Attempt %d/%d: failed to get Stat: %v", attempt, maxAttempts, statErr)
			time.Sleep(interval)
			continue
		}

		currentSize := currentStat.Size()

		// Check size stability
		if currentSize == previousSize {
			stableSizeCount++
		} else {
			// Size changed - file is still downloading
			progress := float64(currentSize) * 100.0 / float64(expectedSize)
			logrus.Infof("📥 [GDRIVE-CACHE-PROGRESS] Attempt %d/%d: size %d -> %d (%.1f%%), waiting for stability...",
				attempt, maxAttempts, previousSize, currentSize, progress)
			previousSize = currentSize
			stableSizeCount = 0
		}

		// Step 2: Test ReadAt from end of file (where cache loads last)
		testOffset := currentSize - int64(len(testBuf))
		if testOffset < 0 {
			testOffset = 0
		}
		readSize := int(currentSize - testOffset)
		if readSize > len(testBuf) {
			readSize = len(testBuf)
		}
		if readSize <= 0 {
			readSize = int(currentSize)
			testOffset = 0
		}

		readStart := time.Now()
		n, readErr := file.ReadAt(testBuf[:readSize], testOffset)
		readDuration := time.Since(readStart)

		if readErr != nil && readErr != io.EOF {
			// ESPIPE or other error - cache is not ready
			if attempt%50 == 0 { // Log every 10 seconds
				logrus.Infof("⏳ [GDRIVE-CACHE-WAIT] Attempt %d/%d (%.1f sec): ReadAt error %v, size stable %d/%d times",
					attempt, maxAttempts, float64(attempt)*interval.Seconds(), readErr, stableSizeCount, requiredStableChecks)
			}
			time.Sleep(interval)
			continue
		}

		// Step 3: Check read speed
		// From local cache should be >>10 MB/s
		// Over Google Drive network usually <5 MB/s
		bytesPerSecond := float64(n) / readDuration.Seconds()
		mbPerSecond := bytesPerSecond / 1024 / 1024

		// Readiness condition: size is stable multiple times in a row AND ReadAt is fast
		if stableSizeCount >= requiredStableChecks && mbPerSecond > minReadSpeedMBps {
			logrus.Infof("✅ [GDRIVE-CACHE-READY] Cache ready after attempt %d/%d (%.1f sec)",
				attempt, maxAttempts, float64(attempt)*interval.Seconds())
			logrus.Infof("   📊 File size: %d bytes (stable %d checks in a row)", currentSize, stableSizeCount)
			logrus.Infof("   📊 ReadAt speed: %.2f MB/s (read %d bytes in %v)", mbPerSecond, n, readDuration)
			return nil
		}

		// Log detailed progress every 10 seconds
		if attempt%50 == 0 {
			elapsed := float64(attempt) * interval.Seconds()
			logrus.Infof("⏳ [GDRIVE-CACHE-WAIT] Attempt %d/%d (%.1f sec):", attempt, maxAttempts, elapsed)
			logrus.Infof("   📊 Size: %d bytes (stable %d/%d times)", currentSize, stableSizeCount, requiredStableChecks)
			logrus.Infof("   📊 ReadAt speed: %.2f MB/s (need >%.1f MB/s for cache)", mbPerSecond, minReadSpeedMBps)
			if stableSizeCount >= requiredStableChecks {
				logrus.Infof("   ⚠️  Size is stable, but speed is low - file might still be downloading")
			} else if mbPerSecond > minReadSpeedMBps {
				logrus.Infof("   ⚠️  Speed is good, but size is still growing - waiting for stabilization")
			}
		}

		time.Sleep(interval)
	}

	// Timeout - cache not ready within allotted time
	return fmt.Errorf("cache not ready after %d attempts (%v)", maxAttempts, maxWaitTime)
}
