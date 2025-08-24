package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/latestcomment/go-websocket-chat/internal/handlers"
	"github.com/latestcomment/go-websocket-chat/internal/models"
	"github.com/latestcomment/go-websocket-chat/internal/services"
)

func main() {
	engine := html.New("./static", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})
	app.Use(logger.New())

	manager := &models.ChannelManager{Channels: make(map[string]*models.Channel)}
	service := services.NewChannelService(manager)
	h := handlers.NewHandler(service)
	ws := handlers.NewWebSocketHandler(service)


	// WebSocket route
	app.Get("/", h.LoginPage)
	app.Post("/channel", h.ChannelPage)
	app.Post("/create-channel", h.CreateChannelPage)
	app.Post("/watch-channel", h.WatchChannel)
	app.Post("/join-channel", h.JoinChannel)
	app.Post("/chat", h.ChatPage)
	app.Get("/ws/:channel/:name/:password?", ws.WebSocketMiddleware, websocket.New(ws.HandleWebSocket))

	log.Println("🚀 Fiber WebSocket server running on :3000")
	log.Fatal(app.Listen(":3000"))
}
