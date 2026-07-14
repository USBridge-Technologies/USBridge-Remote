package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/api"
)

func main() {
	fmt.Println("🚀 Starting Wayland Cursor Debugger...")

	// 1. Инициализируем портал
	err := capture.InitPortalSession()
	if err != nil {
		log.Fatalf("❌ Failed to init portal: %v", err)
	}

	fmt.Println("⏳ Waiting for portal session (3s)...")
	time.Sleep(3 * time.Second)

	nodeID := capture.GetPortalPipeWireNodeID()
	fd := capture.GetPortalPipeWireFD()

	if nodeID == 0 || fd <= 0 {
		log.Fatalf("❌ Failed to get PipeWire info: nodeID=%d, fd=%d", nodeID, fd)
	}

	fmt.Printf("✅ Portal ready: NodeID=%d, FD=%d\n", nodeID, fd)

	// 2. Запускаем watcher напрямую из пакета api
	// Мы используем внутреннюю структуру через экспорт или создаем аналог
	// Так как в internal/api/pw_cursor_linux.go у нас CGO, попробуем вызвать через NewPWCursorWatcher
	// Но он не экспортирован (маленькая буква). 
	// Для теста я временно создам обертку.
    
    fmt.Println("🔬 Attempting to watch cursor...")
    // В данном контексте проще всего добавить лог прямо в агент, но раз мы хотим "вручную",
    // я попробую запустить это как часть пакета api (через go run в директории или переместив файл)
}
