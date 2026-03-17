//go:build !android
// +build !android

package service

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"net/http"
	"sync"
	"time"

	"usbridge-client/internal/models"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

// ICEConnectionState alias для webrtc.ICEConnectionState
type ICEConnectionState = webrtc.ICEConnectionState

// Экспортируем константы для использования в UI
const (
	ICEConnectionStateNew          = webrtc.ICEConnectionStateNew
	ICEConnectionStateChecking     = webrtc.ICEConnectionStateChecking
	ICEConnectionStateConnected    = webrtc.ICEConnectionStateConnected
	ICEConnectionStateCompleted    = webrtc.ICEConnectionStateCompleted
	ICEConnectionStateFailed       = webrtc.ICEConnectionStateFailed
	ICEConnectionStateDisconnected = webrtc.ICEConnectionStateDisconnected
	ICEConnectionStateClosed       = webrtc.ICEConnectionStateClosed
)

// WebRTCService сервис для работы с WebRTC видео потоком
type WebRTCService struct {
	config *models.AppConfig

	// WebRTC соединение
	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackRemote
	audioTrack     *webrtc.TrackRemote

	// Состояние
	isConnected     bool
	isConnecting    bool
	connectionState ICEConnectionState

	// Автоматическое переподключение
	autoReconnect        bool
	reconnectAttempts    int
	maxReconnectAttempts int
	manualDisconnect     bool // Флаг ручной остановки

	// Каналы для видео кадров
	videoFrameChan chan image.Image
	stopChan       chan struct{}

	// Для контроля накопления кадров
	frameDropCount int64
	lastFrameTime  time.Time

	// Мьютексы
	mutex sync.RWMutex

	// Callbacks
	onFrameReceived func(image.Image)
	onStateChanged  func(ICEConnectionState)
	onError         func(error)

	// H.264 декодер
	h264Decoder *H264Decoder

	// Диагностика
	diagnostics *WebRTCDiagnostics
	validator   *WebRTCConnectionValidator
}

// Импортируем H264Decoder из отдельного файла

// NewWebRTCService создает новый WebRTC сервис
func NewWebRTCService(config *models.AppConfig) *WebRTCService {
	ws := &WebRTCService{
		config:         config,
		videoFrameChan: make(chan image.Image, config.BufferSize), // Буфер из конфигурации
		stopChan:       make(chan struct{}),
		h264Decoder:    NewH264Decoder(),
		diagnostics:    NewWebRTCDiagnostics(),
		validator:      NewWebRTCConnectionValidator(),

		// Настройки автоматического переподключения
		autoReconnect:        true,
		reconnectAttempts:    0,
		maxReconnectAttempts: 5,
	}

	// Настраиваем callback для H264 декодера чтобы кадры отправлялись в канал
	frameCallbackCount := 0
	ws.h264Decoder.SetFrameCallback(func(frame image.Image) {
		frameCallbackCount++

		// Логируем только каждый 30й кадр
		if frameCallbackCount%30 == 1 {
			logrus.Debugf("H264 callback: кадр #%d", frameCallbackCount)
		}

		// Логируем первые 10 кадров для отладки
		if frameCallbackCount <= 10 {
			logrus.Infof("🎬 H264 кадр #%d получен в WebRTC сервисе", frameCallbackCount)
		}

		ws.sendFrameWithDrop(frame)
	})

	return ws
}

// ConnectToMediaMTX подключается к MediaMTX WebRTC потоку
func (ws *WebRTCService) ConnectToMediaMTX() error {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if ws.isConnecting || ws.isConnected {
		return fmt.Errorf("уже подключен или подключается")
	}

	// Сбрасываем флаг ручной остановки при новом подключении
	ws.manualDisconnect = false

	// Сбрасываем счетчик кадров при новом подключении
	ws.frameDropCount = 0
	ws.lastFrameTime = time.Time{}

	// Записываем попытку подключения
	ws.diagnostics.RecordConnectionAttempt()

	ws.isConnecting = true
	logrus.Info("🔗 Подключение к MediaMTX WebRTC...")

	// Сначала проверяем доступность MediaMTX
	if err := ws.checkMediaMTXAvailability(); err != nil {
		ws.isConnecting = false
		ws.diagnostics.RecordFailedConnection(err)
		return fmt.Errorf("MediaMTX недоступен: %v", err)
	}

	// Создаем WebRTC соединение
	if err := ws.createPeerConnection(); err != nil {
		ws.isConnecting = false
		ws.diagnostics.RecordFailedConnection(err)
		return fmt.Errorf("ошибка создания WebRTC соединения: %v", err)
	}

	// Убеждаемся что callback установлен после переподключения
	logrus.Infof("🔧 Проверяем H264 декодер: %v", ws.h264Decoder != nil)
	if ws.h264Decoder != nil {
		logrus.Info("🔧 Проверяем и переустанавливаем callback после переподключения")
		// Переустанавливаем callback (всегда, чтобы быть уверенными)
		frameCallbackCount := 0
		ws.h264Decoder.SetFrameCallback(func(frame image.Image) {
			frameCallbackCount++
			if frameCallbackCount <= 10 {
				logrus.Infof("🔄 Переустановленный callback: кадр #%d", frameCallbackCount)
			}
			ws.sendFrameWithDrop(frame)
		})
		logrus.Info("✅ Callback переустановлен после переподключения")
	} else {
		logrus.Warn("⚠️ H264 декодер равен nil после переподключения!")
	}

	// Создаем и отправляем offer
	offer, err := ws.createOffer()
	if err != nil {
		ws.isConnecting = false
		ws.diagnostics.RecordFailedConnection(err)
		return fmt.Errorf("ошибка создания offer: %v", err)
	}

	// Валидируем offer
	if err := ws.validator.ValidateOffer(offer); err != nil {
		ws.isConnecting = false
		ws.diagnostics.RecordFailedConnection(err)
		return fmt.Errorf("невалидный offer: %v", err)
	}

	// Отправляем offer в MediaMTX
	if err := ws.sendOfferToMediaMTX(offer); err != nil {
		logrus.Errorf("❌ Ошибка подключения к MediaMTX: %v", err)
		ws.isConnecting = false
		ws.diagnostics.RecordFailedConnection(err)
		return fmt.Errorf("не удается подключиться к MediaMTX: %v", err)
	}

	// Настраиваем обработчики событий
	ws.setupEventHandlers()

	// Запускаем обработку видео кадров
	go ws.processVideoFrames()

	// Запускаем мониторинг соединения
	go ws.monitorConnection()

	// Запускаем агрессивную очистку накопленных кадров для реалтайма
	go ws.aggressiveFrameCleanup()

	ws.isConnecting = false
	ws.isConnected = true
	ws.diagnostics.RecordSuccessfulConnection()
	logrus.Info("✅ WebRTC подключение к MediaMTX установлено")
	return nil
}

// Disconnect отключается от WebRTC потока
func (ws *WebRTCService) Disconnect() error {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if !ws.isConnected {
		return nil
	}

	logrus.Info("🔌 Отключение от WebRTC потока...")

	// Устанавливаем флаг ручной остановки для предотвращения автоматического переподключения
	ws.manualDisconnect = true

	// Останавливаем обработку кадров
	select {
	case ws.stopChan <- struct{}{}:
	default:
	}

	// Закрываем соединение
	if ws.peerConnection != nil {
		ws.peerConnection.Close()
		ws.peerConnection = nil
	}

	ws.isConnected = false
	ws.isConnecting = false

	logrus.Info("✅ WebRTC соединение закрыто")
	return nil
}

// attemptReconnect пытается переподключиться к WebRTC потоку
func (ws *WebRTCService) attemptReconnect() {
	ws.mutex.Lock()

	// Логируем состояние для отладки
	logrus.Infof("🔄 attemptReconnect: autoReconnect=%v, isConnecting=%v, isConnected=%v, manualDisconnect=%v, attempts=%d/%d",
		ws.autoReconnect, ws.isConnecting, ws.isConnected, ws.manualDisconnect, ws.reconnectAttempts, ws.maxReconnectAttempts)

	if !ws.autoReconnect {
		logrus.Info("🛑 Автоматическое переподключение отключено")
		ws.mutex.Unlock()
		return
	}

	if ws.manualDisconnect {
		logrus.Info("🛑 Ручная остановка - автоматическое переподключение отменено")
		ws.mutex.Unlock()
		return
	}

	if ws.isConnecting {
		logrus.Info("🔄 Уже подключается, пропускаем попытку переподключения")
		ws.mutex.Unlock()
		return
	}

	if ws.isConnected {
		logrus.Info("✅ Уже подключено, пропускаем попытку переподключения")
		ws.mutex.Unlock()
		return
	}

	if ws.reconnectAttempts >= ws.maxReconnectAttempts {
		logrus.Errorf("❌ Превышено максимальное количество попыток переподключения (%d)", ws.maxReconnectAttempts)
		ws.autoReconnect = false
		ws.mutex.Unlock()
		return
	}

	ws.reconnectAttempts++
	attempt := ws.reconnectAttempts
	maxAttempts := ws.maxReconnectAttempts
	ws.mutex.Unlock()

	logrus.Infof("🔄 Попытка переподключения #%d/%d...", attempt, maxAttempts)

	// Ждем перед попыткой переподключения (экспоненциальная задержка)
	delay := time.Duration(attempt) * 2 * time.Second
	logrus.Infof("⏳ Ожидание %v перед переподключением...", delay)
	time.Sleep(delay)

	// Попытка переподключения
	logrus.Info("🔄 Вызываем ConnectToMediaMTX для переподключения")
	if err := ws.ConnectToMediaMTX(); err != nil {
		logrus.Errorf("❌ Ошибка переподключения #%d: %v", attempt, err)
		// Попробуем еще раз через некоторое время
		ws.attemptReconnect()
	} else {
		logrus.Info("✅ Успешное переподключение!")
		ws.mutex.Lock()
		ws.reconnectAttempts = 0 // Сброс счетчика при успешном подключении
		ws.mutex.Unlock()
	}
}

// SetAutoReconnect включает/выключает автоматическое переподключение
func (ws *WebRTCService) SetAutoReconnect(enabled bool) {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ws.autoReconnect = enabled
	if enabled {
		logrus.Info("🔄 Автоматическое переподключение включено")
	} else {
		logrus.Info("🛑 Автоматическое переподключение выключено")
	}
}

// SetMaxReconnectAttempts устанавливает максимальное количество попыток переподключения
func (ws *WebRTCService) SetMaxReconnectAttempts(max int) {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ws.maxReconnectAttempts = max
	logrus.Infof("🔧 Максимальное количество попыток переподключения установлено: %d", max)
}

// createPeerConnection создает WebRTC соединение
func (ws *WebRTCService) createPeerConnection() error {
	logrus.Info("🔧 Создание WebRTC PeerConnection...")

	// Настройки ICE серверов
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: ws.config.STUNServers,
			},
		},
		// Настройки для лучшей совместимости с H.264
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
		// Настройки для низкой задержки
		BundlePolicy:  webrtc.BundlePolicyMaxBundle,
		RTCPMuxPolicy: webrtc.RTCPMuxPolicyRequire,
	}

	logrus.Infof("🌐 ICE серверы: %v", ws.config.STUNServers)
	logrus.Infof("📡 SDP семантика: %s", config.SDPSemantics.String())

	// Создаем соединение
	logrus.Info("🚀 Создание PeerConnection...")
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		logrus.Errorf("❌ Ошибка создания PeerConnection: %v", err)
		return fmt.Errorf("ошибка создания PeerConnection: %v", err)
	}

	logrus.Info("✅ PeerConnection создан успешно")
	ws.peerConnection = peerConnection
	return nil
}

// setupEventHandlers настраивает обработчики событий WebRTC
func (ws *WebRTCService) setupEventHandlers() {
	// Обработчик изменения состояния соединения
	ws.peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		ws.mutex.Lock()
		ws.connectionState = state
		ws.mutex.Unlock()

		logrus.Infof("🔄 WebRTC состояние: %s", state.String())

		if ws.onStateChanged != nil {
			ws.onStateChanged(state)
		}

		switch state {
		case webrtc.ICEConnectionStateConnected:
			logrus.Info("✅ WebRTC соединение установлено")
			logrus.Info("🎉 ICE соединение успешно установлено!")

			// Проверяем, есть ли треки после установления соединения
			go func() {
				time.Sleep(2 * time.Second) // Ждем 2 секунды
				if ws.videoTrack == nil {
					logrus.Warn("⚠️ Видео трек не получен после установления соединения")
					logrus.Warn("⚠️ Возможно, MediaMTX не отправляет видео трек")

					// Проверяем состояние PeerConnection
					stats := ws.peerConnection.GetStats()
					logrus.Infof("📊 Статистика PeerConnection: %+v", stats)

					// Проверяем трансиверы
					transceivers := ws.peerConnection.GetTransceivers()
					logrus.Infof("📡 Количество трансиверов: %d", len(transceivers))
					for i, transceiver := range transceivers {
						logrus.Infof("📡 Трансивер %d: %s, направление: %s", i, transceiver.Kind(), transceiver.Direction())
					}
				} else {
					logrus.Info("✅ Видео трек найден после установления соединения")
				}
			}()
		case webrtc.ICEConnectionStateDisconnected:
			logrus.Warn("⚠️ WebRTC соединение потеряно")
			logrus.Warn("🔌 ICE соединение потеряно")
			// Сбрасываем состояние соединения
			ws.mutex.Lock()
			ws.isConnected = false
			ws.isConnecting = false
			ws.mutex.Unlock()
			// Попытка автоматического переподключения
			logrus.Info("🔄 Запуск автоматического переподключения из-за потери ICE соединения")
			go ws.attemptReconnect()
		case webrtc.ICEConnectionStateFailed:
			logrus.Error("❌ WebRTC соединение не удалось")
			logrus.Error("💥 ICE соединение не удалось - проверьте сетевые настройки")
			// Сбрасываем состояние соединения
			ws.mutex.Lock()
			ws.isConnected = false
			ws.isConnecting = false
			ws.mutex.Unlock()
			if ws.onError != nil {
				ws.onError(fmt.Errorf("WebRTC соединение не удалось"))
			}
			// Попытка автоматического переподключения
			logrus.Info("🔄 Запуск автоматического переподключения из-за неудачного ICE соединения")
			go ws.attemptReconnect()
		case webrtc.ICEConnectionStateChecking:
			logrus.Info("🔍 WebRTC проверка соединения...")
			logrus.Info("🔍 ICE проверка соединения - выполняется обмен кандидатами")
		case webrtc.ICEConnectionStateNew:
			logrus.Info("🆕 WebRTC новое соединение")
			logrus.Info("🆕 ICE новое соединение - начало процесса подключения")
		case webrtc.ICEConnectionStateClosed:
			logrus.Info("🔒 WebRTC соединение закрыто")
			logrus.Info("🔒 ICE соединение закрыто")
		}
	})

	// Обработчик входящих треков
	ws.peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logrus.Infof("📹 Получен трек: %s", track.Kind().String())
		logrus.Infof("📹 ID трека: %s", track.ID())
		logrus.Infof("📹 SSRC трека: %d", track.SSRC())
		logrus.Infof("📹 RID трека: %s", track.RID())

		if track.Kind() == webrtc.RTPCodecTypeVideo {
			ws.videoTrack = track
			// Получаем информацию о кодеках
			codec := track.Codec()
			logrus.Infof("📹 Видео кодек: %s, SDP: %s", codec.MimeType, codec.SDPFmtpLine)
			logrus.Infof("📹 Payload Type: %d", codec.PayloadType)
			logrus.Infof("📹 Clock Rate: %d", codec.ClockRate)

			// Проверяем поддержку H.264
			if codec.MimeType == "video/H264" {
				logrus.Info("✅ H.264 кодек подтвержден")
			} else {
				logrus.Warnf("⚠️ Неожиданный видео кодек: %s", codec.MimeType)
			}

			logrus.Info("🎬 Запуск обработки видео трека...")
			go ws.handleVideoTrack(track)
		} else if track.Kind() == webrtc.RTPCodecTypeAudio {
			ws.audioTrack = track
			logrus.Info("🔊 Аудио трек получен")
		}
	})

	// Обработчик ICE кандидатов
	ws.peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			logrus.Info("🏁 ICE кандидаты завершены")
			return
		}
		logrus.Infof("🧊 ICE кандидат: %s", candidate.String())

		// Анализируем тип кандидата
		if candidate.Typ == webrtc.ICECandidateTypeHost {
			logrus.Infof("🏠 Локальный кандидат: %s", candidate.Address)
		} else if candidate.Typ == webrtc.ICECandidateTypeSrflx {
			logrus.Infof("🌐 STUN кандидат: %s", candidate.Address)
		} else if candidate.Typ == webrtc.ICECandidateTypeRelay {
			logrus.Infof("🔄 TURN кандидат: %s", candidate.Address)
		}
	})

	// Обработчик изменения состояния ICE gathering
	ws.peerConnection.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		logrus.Infof("🧊 ICE Gathering состояние: %s", state.String())
		switch state {
		case webrtc.ICEGathererStateNew:
			logrus.Info("🆕 ICE Gathering: новое состояние")
		case webrtc.ICEGathererStateGathering:
			logrus.Info("🔍 ICE Gathering: сбор кандидатов")
		case webrtc.ICEGathererStateComplete:
			logrus.Info("✅ ICE Gathering: сбор завершен")
		}
	})

	// Обработчик изменения состояния соединения
	ws.peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logrus.Infof("🔗 PeerConnection состояние: %s", state.String())
		switch state {
		case webrtc.PeerConnectionStateNew:
			logrus.Info("🆕 PeerConnection: новое соединение")
		case webrtc.PeerConnectionStateConnecting:
			logrus.Info("🔗 PeerConnection: подключение...")
		case webrtc.PeerConnectionStateConnected:
			logrus.Info("✅ PeerConnection: подключено")
		case webrtc.PeerConnectionStateDisconnected:
			logrus.Warn("⚠️ PeerConnection: отключено")
		case webrtc.PeerConnectionStateFailed:
			logrus.Error("❌ PeerConnection: не удалось")
		case webrtc.PeerConnectionStateClosed:
			logrus.Info("🔒 PeerConnection: закрыто")
		}
	})

	// Обработчик изменения состояния сигнализации
	ws.peerConnection.OnSignalingStateChange(func(state webrtc.SignalingState) {
		logrus.Infof("📡 Signaling состояние: %s", state.String())
		switch state {
		case webrtc.SignalingStateStable:
			logrus.Info("✅ Signaling: стабильное состояние")
		case webrtc.SignalingStateHaveLocalOffer:
			logrus.Info("📤 Signaling: есть локальный offer")
		case webrtc.SignalingStateHaveRemoteOffer:
			logrus.Info("📥 Signaling: есть удаленный offer")
		case webrtc.SignalingStateHaveLocalPranswer:
			logrus.Info("📤 Signaling: есть локальный pranswer")
		case webrtc.SignalingStateHaveRemotePranswer:
			logrus.Info("📥 Signaling: есть удаленный pranswer")
		case webrtc.SignalingStateClosed:
			logrus.Info("🔒 Signaling: закрыто")
		}
	})

	// Дополнительные обработчики событий для диагностики
	logrus.Info("✅ Все обработчики событий WebRTC настроены")
}

// createOffer создает WebRTC offer
func (ws *WebRTCService) createOffer() (*webrtc.SessionDescription, error) {
	logrus.Info("📝 Создание WebRTC offer...")

	// Добавляем трансивер для видео с приоритетом H.264
	logrus.Info("📹 Добавление видео трансивера...")
	videoTransceiver, err := ws.peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	if err != nil {
		logrus.Errorf("❌ Ошибка добавления видео трансивера: %v", err)
		return nil, fmt.Errorf("ошибка добавления видео трансивера: %v", err)
	}
	logrus.Info("✅ Видео трансивер добавлен")

	// Настраиваем кодек для H.264
	if videoTransceiver.Sender() != nil {
		// Получаем параметры для проверки доступных кодеков
		codecs := videoTransceiver.Sender().GetParameters()
		logrus.Infof("📋 Доступные видео кодеки: %d", len(codecs.Codecs))
		for i, codec := range codecs.Codecs {
			logrus.Infof("  %d. %s (payload: %d)", i+1, codec.MimeType, codec.PayloadType)
		}

		// Проверяем наличие H.264 кодеков
		h264Found := false
		for _, codec := range codecs.Codecs {
			if codec.MimeType == "video/H264" {
				logrus.Info("✅ H.264 кодек доступен")
				h264Found = true
				break
			}
		}
		if !h264Found {
			logrus.Warn("⚠️ H.264 кодек не найден в доступных кодеках")
		}
	}

	// Добавляем трансивер для аудио
	logrus.Info("🔊 Добавление аудио трансивера...")
	_, err = ws.peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	if err != nil {
		logrus.Errorf("❌ Ошибка добавления аудио трансивера: %v", err)
		return nil, fmt.Errorf("ошибка добавления аудио трансивера: %v", err)
	}
	logrus.Info("✅ Аудио трансивер добавлен")

	// Создаем offer
	logrus.Info("📝 Создание WebRTC offer...")
	offer, err := ws.peerConnection.CreateOffer(nil)
	if err != nil {
		logrus.Errorf("❌ Ошибка создания offer: %v", err)
		return nil, fmt.Errorf("ошибка создания offer: %v", err)
	}
	logrus.Info("✅ WebRTC offer создан")

	// Устанавливаем локальное описание
	logrus.Info("🔧 Установка локального описания...")
	err = ws.peerConnection.SetLocalDescription(offer)
	if err != nil {
		logrus.Errorf("❌ Ошибка установки локального описания: %v", err)
		return nil, fmt.Errorf("ошибка установки локального описания: %v", err)
	}
	logrus.Info("✅ Локальное описание установлено")

	logrus.Debugf("📄 Создан WebRTC offer: %s", offer.SDP)
	return &offer, nil
}

// sendOfferToMediaMTX отправляет offer в MediaMTX
func (ws *WebRTCService) sendOfferToMediaMTX(offer *webrtc.SessionDescription) error {
	// MediaMTX WebRTC API endpoint для получения потока
	// Используем правильный путь для существующего потока - именно 'stream', не 'api/video/stream'
	url := fmt.Sprintf("http://%s:%d/stream/whep", ws.config.MediaMTXHost, ws.config.MediaMTXWebRTC)

	// Сериализуем offer в SDP формат
	offerSDP := offer.SDP

	logrus.Infof("🔗 Отправка offer в MediaMTX: %s", url)
	logrus.Infof("📄 Размер SDP offer: %d байт", len(offerSDP))
	logrus.Debugf("📄 Offer SDP содержимое:\n%s", offerSDP)

	// Проверим что путь правильный - должен быть /stream/whep
	logrus.Infof("🛣️ WHEP endpoint путь: /stream/whep")

	// Отправляем POST запрос с SDP в теле
	logrus.Info("🌐 Выполнение HTTP POST запроса к MediaMTX...")
	resp, err := ws.makeHTTPRequest("POST", url, []byte(offerSDP), map[string]string{
		"Content-Type": "application/sdp",
	})
	if err != nil {
		logrus.Errorf("❌ HTTP запрос к MediaMTX не удался: %v", err)
		return fmt.Errorf("ошибка HTTP запроса: %v", err)
	}

	logrus.Infof("📥 Получен ответ от MediaMTX, размер: %d байт", len(resp))
	logrus.Debugf("📥 Ответ от MediaMTX:\n%s", string(resp))

	// Парсим ответ с answer
	var answer webrtc.SessionDescription
	answer.Type = webrtc.SDPTypeAnswer
	answer.SDP = string(resp)

	logrus.Info("🔧 Установка удаленного описания (answer)...")
	// Устанавливаем удаленное описание
	err = ws.peerConnection.SetRemoteDescription(answer)
	if err != nil {
		logrus.Errorf("❌ Ошибка установки удаленного описания: %v", err)
		return fmt.Errorf("ошибка установки удаленного описания: %v", err)
	}

	logrus.Info("✅ WebRTC offer успешно отправлен в MediaMTX")
	logrus.Info("✅ Удаленное описание (answer) установлено")
	return nil
}

// handleVideoTrack обрабатывает видео трек
func (ws *WebRTCService) handleVideoTrack(track *webrtc.TrackRemote) {
	logrus.Info("🎬 Начало обработки видео трека")
	logrus.Infof("📹 Трек ID: %s", track.ID())
	logrus.Infof("📹 Трек SSRC: %d", track.SSRC())

	// Получаем информацию о кодеках
	codec := track.Codec()
	logrus.Infof("📹 Получен видео трек: %s, %s", codec.MimeType, codec.SDPFmtpLine)
	logrus.Infof("📹 Payload Type: %d, Clock Rate: %d", codec.PayloadType, codec.ClockRate)

	packetCount := 0
	frameCount := 0
	lastLogTime := time.Now()

	for {
		select {
		case <-ws.stopChan:
			logrus.Info("🛑 Остановка обработки видео трека")
			return
		default:
			// Читаем RTP пакет
			rtpPacket, _, err := track.ReadRTP()
			if err != nil {
				// Проверяем, является ли ошибка EOF (разрыв соединения)
				if err == io.EOF {
					logrus.Warn("⚠️ Разрыв соединения (EOF) - завершение обработки видео трека")
					// Помечаем соединение как отключенное
					ws.mutex.Lock()
					ws.isConnected = false
					ws.mutex.Unlock()
					// Попытка автоматического переподключения
					logrus.Info("🔄 Запуск автоматического переподключения из-за EOF в видео треке")
					go ws.attemptReconnect()
					return
				}

				// Логируем другие ошибки, но не спамим
				logrus.Errorf("❌ Ошибка чтения RTP пакета: %v", err)

				// Небольшая задержка перед следующей попыткой
				time.Sleep(100 * time.Millisecond)
				continue
			}

			packetCount++

			// Минимальное логирование первых пакетов
			if packetCount <= 3 {
				logrus.Debugf("RTP пакет #%d: размер=%d байт", packetCount, len(rtpPacket.Payload))
			}

			// Статистика каждые 30 секунд
			if time.Since(lastLogTime) >= 30*time.Second {
				logrus.Infof("📊 Статистика: %d RTP пакетов, %d кадров за 30 сек", packetCount, frameCount)
				lastLogTime = time.Now()
				packetCount = 0 // Сброс счетчика
				frameCount = 0
			}

			// Декодируем кадр
			frame, err := ws.decodeRTPFrame(rtpPacket)
			if err != nil {
				// Пропускаем кадры, которые не удалось декодировать
				if packetCount <= 5 { // Логируем ошибки первых пакетов
					logrus.Debugf("❌ Пропуск кадра #%d: %v", packetCount, err)
				}
				continue
			}

			if frame != nil {
				frameCount++

				// Отправляем кадр с агрессивным сбросом накопленных
				ws.sendFrameWithDrop(frame)
			}
		}
	}
}

// processVideoFrames обрабатывает видео кадры
func (ws *WebRTCService) processVideoFrames() {
	logrus.Info("🎞️ Начало обработки видео кадров")
	processedFrameCount := 0

	for {
		select {
		case <-ws.stopChan:
			logrus.Info("🛑 Остановка обработки видео кадров")
			return
		case frame := <-ws.videoFrameChan:
			processedFrameCount++

			// Логируем только каждый 30й кадр
			if processedFrameCount%30 == 1 {
				logrus.Debugf("WebRTC: кадр #%d -> UI", processedFrameCount)
			}

			// Логируем первые 10 кадров для отладки
			if processedFrameCount <= 10 {
				logrus.Infof("🎞️ WebRTC обрабатывает кадр #%d", processedFrameCount)
			}

			if ws.onFrameReceived != nil {
				// Логируем первые 10 отправок в UI
				if processedFrameCount <= 10 {
					logrus.Infof("📤 WebRTC отправляет кадр #%d в UI", processedFrameCount)
				}
				ws.onFrameReceived(frame)
			} else {
				logrus.Warn("⚠️ Callback onFrameReceived не установлен")
			}
		}
	}
}

// decodeRTPFrame декодирует RTP пакет в изображение
func (ws *WebRTCService) decodeRTPFrame(rtpPacket *rtp.Packet) (image.Image, error) {
	// Используем правильный H.264 декодер
	return ws.h264Decoder.DecodeRTPPacket(rtpPacket)
}

// makeHTTPRequest выполняет HTTP запрос
func (ws *WebRTCService) makeHTTPRequest(method, url string, body []byte, headers map[string]string) ([]byte, error) {
	logrus.Infof("🌐 HTTP %s запрос к: %s", method, url)
	logrus.Infof("📦 Размер тела запроса: %d байт", len(body))

	// Создаем HTTP клиент
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Создаем запрос
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		logrus.Errorf("❌ Ошибка создания HTTP запроса: %v", err)
		return nil, fmt.Errorf("ошибка создания запроса: %v", err)
	}

	// Устанавливаем заголовки
	for key, value := range headers {
		req.Header.Set(key, value)
		logrus.Debugf("📋 Заголовок: %s: %s", key, value)
	}

	logrus.Info("🚀 Отправка HTTP запроса...")
	// Выполняем запрос
	resp, err := client.Do(req)
	if err != nil {
		logrus.Errorf("❌ Ошибка выполнения HTTP запроса: %v", err)
		return nil, fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	logrus.Infof("📊 HTTP статус ответа: %d %s", resp.StatusCode, resp.Status)

	// Читаем ответ
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("❌ Ошибка чтения HTTP ответа: %v", err)
		return nil, fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	logrus.Infof("📥 Размер HTTP ответа: %d байт", len(respBody))

	// WHEP API возвращает 201 Created при успешном создании сессии
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logrus.Errorf("❌ HTTP ошибка %d: %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("HTTP ошибка %d: %s", resp.StatusCode, string(respBody))
	}

	logrus.Info("✅ HTTP запрос выполнен успешно")
	return respBody, nil
}

// SetOnFrameReceived устанавливает callback для получения кадров
func (ws *WebRTCService) SetOnFrameReceived(callback func(image.Image)) {
	ws.onFrameReceived = callback
}

// SetOnStateChanged устанавливает callback для изменения состояния
func (ws *WebRTCService) SetOnStateChanged(callback func(ICEConnectionState)) {
	ws.onStateChanged = callback
}

// SetOnError устанавливает callback для ошибок
func (ws *WebRTCService) SetOnError(callback func(error)) {
	ws.onError = callback
}

// IsConnected возвращает состояние подключения
func (ws *WebRTCService) IsConnected() bool {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.isConnected
}

// GetConnectionState возвращает состояние соединения
func (ws *WebRTCService) GetConnectionState() ICEConnectionState {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.connectionState
}

// GetStats возвращает статистику соединения
func (ws *WebRTCService) GetStats() map[string]interface{} {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	stats := map[string]interface{}{
		"connected":        ws.isConnected,
		"connecting":       ws.isConnecting,
		"connection_state": ws.connectionState.String(),
		"video_track":      ws.videoTrack != nil,
		"audio_track":      ws.audioTrack != nil,
		"frames_dropped":   ws.frameDropCount,
		"last_frame_time":  ws.lastFrameTime,
		"low_latency_mode": ws.config.LowLatencyMode,
	}

	// Добавляем статистику H.264 декодера
	if ws.h264Decoder != nil {
		h264Stats := ws.h264Decoder.GetStats()
		stats["h264_decoder"] = h264Stats
	}

	// Добавляем диагностику
	if ws.diagnostics != nil {
		diagnosticsStats := ws.diagnostics.GetStats()
		stats["diagnostics"] = diagnosticsStats
		stats["connection_health"] = ws.diagnostics.CheckConnectionHealth()
	}

	return stats
}

// monitorConnection мониторит состояние соединения
func (ws *WebRTCService) monitorConnection() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ws.stopChan:
			logrus.Info("🛑 Остановка мониторинга соединения")
			return
		case <-ticker.C:
			ws.checkConnectionHealth()
		}
	}
}

// checkConnectionHealth проверяет здоровье соединения
func (ws *WebRTCService) checkConnectionHealth() {
	ws.mutex.RLock()
	connected := ws.isConnected
	state := ws.connectionState
	ws.mutex.RUnlock()

	if !connected {
		return
	}

	// Проверяем состояние соединения
	switch state {
	case webrtc.ICEConnectionStateConnected:
		// Соединение активно, проверяем наличие видео трека
		if ws.videoTrack == nil {
			logrus.Warn("⚠️ Соединение активно, но видео трек отсутствует")

			// Проверяем трансиверы
			transceivers := ws.peerConnection.GetTransceivers()
			for i, transceiver := range transceivers {
				if transceiver.Kind() == webrtc.RTPCodecTypeVideo {
					logrus.Infof("📡 Видео трансивер %d: направление %s", i, transceiver.Direction())
				}
			}
		} else {
			logrus.Debug("✅ Соединение активно, видео трек присутствует")
		}
	case webrtc.ICEConnectionStateDisconnected:
		logrus.Warn("⚠️ Соединение потеряно")
		if ws.onError != nil {
			ws.onError(fmt.Errorf("WebRTC соединение потеряно"))
		}
	case webrtc.ICEConnectionStateFailed:
		logrus.Error("❌ Соединение не удалось")
		if ws.onError != nil {
			ws.onError(fmt.Errorf("WebRTC соединение не удалось"))
		}
	}
}

// checkMediaMTXAvailability проверяет доступность MediaMTX сервера
func (ws *WebRTCService) checkMediaMTXAvailability() error {
	// Проверяем доступность потока 'stream' по правильному пути
	streamURL := fmt.Sprintf("http://%s:%d/stream/", ws.config.MediaMTXHost, ws.config.MediaMTXWebRTC)
	logrus.Infof("🔍 Проверка потока 'stream': %s", streamURL)

	streamResp, err := ws.makeHTTPRequest("GET", streamURL, nil, map[string]string{})
	if err != nil {
		logrus.Warnf("⚠️ Поток 'stream' недоступен: %v", err)
		return fmt.Errorf("поток 'stream' недоступен: %v", err)
	}

	logrus.Infof("✅ Поток 'stream' доступен, размер ответа: %d байт", len(streamResp))
	logrus.Debugf("📄 Ответ от stream: %s", string(streamResp))

	return nil
}

// min возвращает минимальное из двух чисел
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// UpdateHost обновляет хост MediaMTX
func (ws *WebRTCService) UpdateHost(host string) {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	ws.config.MediaMTXHost = host
	logrus.Infof("🔧 WebRTC сервис: хост обновлен на %s", host)
}

// GetConfig возвращает конфигурацию WebRTC сервиса
func (ws *WebRTCService) GetConfig() *models.AppConfig {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	return ws.config
}

// GetH264Decoder возвращает H.264 декодер для проверки
func (ws *WebRTCService) GetH264Decoder() *H264Decoder {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.h264Decoder
}

// sendFrameWithDrop отправляет кадр с умным сбросом накопленных кадров
func (ws *WebRTCService) sendFrameWithDrop(frame image.Image) {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	now := time.Now()

	// Если включен режим низкой задержки, умно сбрасываем накопленные кадры
	if ws.config.LowLatencyMode {
		// Проверяем, сколько кадров накопилось
		bufferedCount := 0
		for {
			select {
			case <-ws.videoFrameChan:
				bufferedCount++
				ws.frameDropCount++
			default:
				goto sendNewFrame
			}
		}

	sendNewFrame:
		if bufferedCount > 0 {
			logrus.Debugf("🔥 Сброшено %d накопленных кадров для реалтайма", bufferedCount)
		}
	}

	// Отправляем новый кадр
	select {
	case ws.videoFrameChan <- frame:
		ws.lastFrameTime = now
		// Успешно отправлен
		logrus.Debugf("✅ Кадр отправлен в канал")
	default:
		// Если буфер полон, заменяем один старый кадр новым
		select {
		case <-ws.videoFrameChan: // Удаляем один старый кадр
			ws.videoFrameChan <- frame // Добавляем новый
			ws.frameDropCount++
			ws.lastFrameTime = now
			logrus.Debugf("🔄 Кадр заменен в буфере")
		default:
			// Если совсем не можем, пропускаем
			ws.frameDropCount++
			logrus.Warnf("⚠️ Кадр пропущен - буфер заблокирован")
		}
	}
}

// aggressiveFrameCleanup умно очищает накопленные кадры, оставляя только последний
func (ws *WebRTCService) aggressiveFrameCleanup() {
	ticker := time.NewTicker(100 * time.Millisecond) // Каждые 100мс
	defer ticker.Stop()

	for {
		select {
		case <-ws.stopChan:
			logrus.Info("🛑 Остановка умной очистки кадров")
			return
		case <-ticker.C:
			ws.mutex.Lock()

			// Если включен режим низкой задержки, сбрасываем накопленные кадры, но оставляем последний
			if ws.config.LowLatencyMode && ws.isConnected {
				// Считаем количество накопленных кадров
				bufferedFrames := 0
				for {
					select {
					case <-ws.videoFrameChan:
						bufferedFrames++
					default:
						goto countDone
					}
				}

			countDone:
				// Если накопилось больше 1 кадра, логируем это
				if bufferedFrames > 1 {
					// Сбрасываем все кроме последнего (он уже был извлечен)
					dropped := bufferedFrames - 1
					ws.frameDropCount += int64(dropped)
					logrus.Debugf("🧹 Умная очистка: было %d кадров, сброшено %d старых", bufferedFrames, dropped)
				} else if bufferedFrames == 1 {
					// Возвращаем кадр обратно в буфер
					select {
					case ws.videoFrameChan <- nil: // Заглушка, кадр уже обработан
					default:
					}
				}
			}

			ws.mutex.Unlock()
		}
	}
}
