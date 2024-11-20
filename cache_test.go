package nowpaste_test

import (
	"context"
	"testing"
	"time"

	"github.com/mashiike/nowpaste"
)

func TestInmemoryChannelCache(t *testing.T) {
	cache := nowpaste.NewInmemoryChannelCache()

	// Test Set and Get
	cache.SetMulti(context.TODO(), []nowpaste.ChannelCacheEntry{
		{ChannelName: "key1", ChannelID: "value1", TTL: time.Now().Add(1 * time.Minute)},
	})
	channelID, found, err := cache.Get(context.TODO(), "key1")
	if err != nil || !found || channelID != "value1" {
		t.Errorf("expected value1, got %v", channelID)
	}

	// Test expiration
	cache.SetMulti(context.TODO(), []nowpaste.ChannelCacheEntry{
		{ChannelName: "key2", ChannelID: "value2", TTL: time.Now().Add(1 * time.Second)},
	})
	time.Sleep(2 * time.Second)
	_, found, err = cache.Get(context.TODO(), "key2")
	if err != nil || found {
		t.Errorf("expected key2 to be expired")
	}

	// Test overwrite
	cache.SetMulti(context.TODO(), []nowpaste.ChannelCacheEntry{
		{ChannelName: "key3", ChannelID: "value3", TTL: time.Now().Add(1 * time.Minute)},
	})
	cache.SetMulti(context.TODO(), []nowpaste.ChannelCacheEntry{
		{ChannelName: "key3", ChannelID: "new_value3", TTL: time.Now().Add(1 * time.Minute)},
	})
	channelID, found, err = cache.Get(context.TODO(), "key3")
	if err != nil || !found || channelID != "new_value3" {
		t.Errorf("expected new_value3, got %v", channelID)
	}
}
