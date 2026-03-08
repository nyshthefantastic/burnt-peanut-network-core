package cabi

var registry = &handleRegistry{
	handles: make(map[uintptr]interface{}),
	// this start at 1 because 0 is reserved for nil in C
	next:    1,
}

/*
with lock method we can ensure that only one goroutine can access the registry at a time.
*/
func RegisterHandle(obj interface{}) uintptr {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	handle := registry.next
	registry.handles[handle] = obj
	registry.next++
	return handle
}

func GetHandle(handle uintptr) interface{} {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	return registry.handles[handle]
}

func ReleaseHandle(handle uintptr) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	delete(registry.handles, handle)
}