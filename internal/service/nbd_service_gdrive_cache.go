package service

// Этот файл содержит утилиту для проверки готовности Google Drive кэша
//
// Проблема: io.Copy(io.Discard, file) завершается ДО того как Android полностью закэширует файл.
// Проверка только ReadAt недостаточна - ReadAt может работать пока файл еще загружается.
//
// Решение: Комплексная проверка готовности кэша:
// 1. Проверяем что размер файла стабилен (не растет) - значит загрузка завершена
// 2. Проверяем что ReadAt работает быстро (>10 MB/s) - значит читаем из локального кэша
// 3. Требуем несколько последовательных проверок с стабильным размером

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// WaitForGDriveCache ждет пока Google Drive файл полностью закэшируется Android
//
// Параметры:
// - file: открытый файл через SAF
// - expectedSize: ожидаемый размер файла (из начального Stat)
// - maxWaitTime: максимальное время ожидания
//
// Возвращает nil если кэш готов, error если timeout или другая ошибка
func WaitForGDriveCache(file *os.File, expectedSize int64, maxWaitTime time.Duration) error {
	logrus.Infof("📍 [GDRIVE-CACHE-WAIT] Начало ожидания кэширования файла (ожидаемый размер: %d байт, макс. время: %v)", expectedSize, maxWaitTime)

	testBuf := make([]byte, 65536) // 64KB буфер для теста скорости
	interval := 200 * time.Millisecond
	maxAttempts := int(maxWaitTime / interval)

	// Параметры проверки
	previousSize := expectedSize
	stableSizeCount := 0
	const requiredStableChecks = 3    // Требуется 3 проверки с одинаковым размером подряд
	const minReadSpeedMBps = 10.0     // Минимальная скорость для локального кэша (MB/s)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Шаг 1: Проверяем текущий размер файла
		currentStat, statErr := file.Stat()
		if statErr != nil {
			logrus.Warnf("⚠️ [GDRIVE-CACHE-WAIT] Попытка %d/%d: не удалось получить Stat: %v", attempt, maxAttempts, statErr)
			time.Sleep(interval)
			continue
		}

		currentSize := currentStat.Size()

		// Проверяем стабильность размера
		if currentSize == previousSize {
			stableSizeCount++
		} else {
			// Размер изменился - файл все еще загружается
			progress := float64(currentSize) * 100.0 / float64(expectedSize)
			logrus.Infof("📥 [GDRIVE-CACHE-PROGRESS] Попытка %d/%d: размер %d -> %d (%.1f%%), ждем стабильности...",
				attempt, maxAttempts, previousSize, currentSize, progress)
			previousSize = currentSize
			stableSizeCount = 0
		}

		// Шаг 2: Тестируем ReadAt с конца файла (там кэш догружается последним)
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
			// ESPIPE или другая ошибка - кэш не готов
			if attempt%50 == 0 { // Логируем каждые 10 секунд
				logrus.Infof("⏳ [GDRIVE-CACHE-WAIT] Попытка %d/%d (%.1f сек): ReadAt ошибка %v, размер стабилен %d/%d раз",
					attempt, maxAttempts, float64(attempt)*interval.Seconds(), readErr, stableSizeCount, requiredStableChecks)
			}
			time.Sleep(interval)
			continue
		}

		// Шаг 3: Проверяем скорость чтения
		// Из локального кэша должно быть >>10 MB/s
		// По сети Google Drive обычно <5 MB/s
		bytesPerSecond := float64(n) / readDuration.Seconds()
		mbPerSecond := bytesPerSecond / 1024 / 1024

		// Условие готовности: размер стабилен несколько раз подряд И ReadAt быстрый
		if stableSizeCount >= requiredStableChecks && mbPerSecond > minReadSpeedMBps {
			logrus.Infof("✅ [GDRIVE-CACHE-READY] Кэш готов после попытки %d/%d (%.1f сек)",
				attempt, maxAttempts, float64(attempt)*interval.Seconds())
			logrus.Infof("   📊 Размер файла: %d байт (стабилен %d проверок подряд)", currentSize, stableSizeCount)
			logrus.Infof("   📊 Скорость ReadAt: %.2f MB/s (прочитано %d байт за %v)", mbPerSecond, n, readDuration)
			return nil
		}

		// Логируем детальный прогресс каждые 10 секунд
		if attempt%50 == 0 {
			elapsed := float64(attempt) * interval.Seconds()
			logrus.Infof("⏳ [GDRIVE-CACHE-WAIT] Попытка %d/%d (%.1f сек):", attempt, maxAttempts, elapsed)
			logrus.Infof("   📊 Размер: %d байт (стабилен %d/%d раз)", currentSize, stableSizeCount, requiredStableChecks)
			logrus.Infof("   📊 Скорость ReadAt: %.2f MB/s (нужно >%.1f MB/s для кэша)", mbPerSecond, minReadSpeedMBps)
			if stableSizeCount >= requiredStableChecks {
				logrus.Infof("   ⚠️  Размер стабилен, но скорость низкая - возможно файл еще загружается")
			} else if mbPerSecond > minReadSpeedMBps {
				logrus.Infof("   ⚠️  Скорость хорошая, но размер все еще растет - ждем стабилизации")
			}
		}

		time.Sleep(interval)
	}

	// Timeout - кэш не готов за отведенное время
	return fmt.Errorf("кэш не готов после %d попыток (%v)", maxAttempts, maxWaitTime)
}
