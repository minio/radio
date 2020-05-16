package cmd

import "sync"

// Global object layer mutex, used for safely updating object layer.
var globalObjLayerMutex *sync.RWMutex

// Global object layer, only accessed by globalObjectAPI.
var globalObjectAPI ObjectLayer

//Global cacheObjects, only accessed by newCacheObjectsFn().
var globalCacheObjectAPI CacheObjectLayer

func init() {
	// Initialize this once per server initialization.
	globalObjLayerMutex = &sync.RWMutex{}
}
