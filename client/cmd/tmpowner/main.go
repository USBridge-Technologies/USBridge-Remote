package main

import (
	"crypto/sha256"
	"fmt"
	"os"

	"usbridge-client/internal/api"
)

func main() {
	c := api.NewUSBClient("192.168.200.15", 8080, 60)
	_ = sha256.Sum256
	c.SetAPISecretV2([]byte(os.Getenv("MASTERKEY")))
	switch os.Args[1] {
	case "invite":
		link, sent, err := c.InviteGuest(api.GuestInvite{Email: os.Args[2], Role: "operator", ExpiresIn: 3600, RequireConsent: false})
		fmt.Println("invite:", link, sent, err)
	case "list":
		l, err := c.ListGuests()
		if err != nil {
			fmt.Println("list err:", err)
			return
		}
		for _, g := range l.Guests {
			fmt.Println(g.ID, g.Email, g.Role, g.Live)
		}
	case "revoke":
		fmt.Println("revoke:", c.RevokeGuest(os.Args[2]))
	}
}
