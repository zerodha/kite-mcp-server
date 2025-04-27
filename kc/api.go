package kc

import (
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

type KiteConnect struct {
	// Add fields here
	Client *kiteconnect.Client // TODO: this can be made private ?
}

func NewKiteConnect(apiKey string) *KiteConnect {
	client := kiteconnect.New(apiKey)

	return &KiteConnect{
		Client: client,
	}
}
