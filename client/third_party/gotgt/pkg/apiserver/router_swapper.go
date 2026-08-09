/*
Copyright 2016 The GoStor Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package apiserver contains the code that provides a rest.ful api service.
package apiserver

import (
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

// routerSwapper is an http.Handler that allow you to swap
// mux routers.
type routerSwapper struct {
	mu     sync.Mutex
	router *mux.Router
}

// Swap changes the old router with the new one.
func (rs *routerSwapper) Swap(newRouter *mux.Router) {
	rs.mu.Lock()
	rs.router = newRouter
	rs.mu.Unlock()
}

// ServeHTTP makes the routerSwapper to implement the http.Handler interface.
func (rs *routerSwapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rs.mu.Lock()
	router := rs.router
	rs.mu.Unlock()
	router.ServeHTTP(w, r)
}
