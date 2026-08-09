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
package lu

import (
	"net/http"

	"github.com/gostor/gotgt/pkg/apiserver/router"
	"golang.org/x/net/context"
)

// containerRouter is a router to talk with the container controller
type luRouter struct {
	routes []router.Route
}

// NewRouter initializes a new container router
func NewRouter() router.Router {
	r := &luRouter{}
	r.initRoutes()
	return r
}

// Routes returns the available routers to the container controller
func (r *luRouter) Routes() []router.Route {
	return r.routes
}

// initRoutes initializes the routes in lu router
func (r *luRouter) initRoutes() {
	r.routes = []router.Route{
		// GET
		router.NewGetRoute("/lu/{id:.*}", r.getLu),
		// POST
		router.NewPostRoute("/lu/create", r.postLuCreate),
		// PUT
		// DELETE
		router.NewDeleteRoute("/lu/{id:.*}", r.deleteLu),
	}
}

func (s *luRouter) getLu(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return nil
}

func (s *luRouter) postLuCreate(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return nil
}

func (s *luRouter) deleteLu(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return nil
}
