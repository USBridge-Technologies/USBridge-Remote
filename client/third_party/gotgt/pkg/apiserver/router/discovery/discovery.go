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
package discovery

import (
	"net/http"

	"github.com/gostor/gotgt/pkg/apiserver/router"
	"golang.org/x/net/context"
)

// containerRouter is a router to talk with the container controller
type discoveryRouter struct {
	routes []router.Route
}

// NewRouter initializes a new container router
func NewRouter() router.Router {
	r := &discoveryRouter{}
	r.initRoutes()
	return r
}

// Routes returns the available routers to the container controller
func (r *discoveryRouter) Routes() []router.Route {
	return r.routes
}

// initRoutes initializes the routes in discovery router
func (r *discoveryRouter) initRoutes() {
	r.routes = []router.Route{
		// GET
		router.NewGetRoute("/discovery/{name:.*}", r.getDiscovery),
		// POST
		router.NewPostRoute("/discovery/create", r.postDiscoveryCreate),
		// PUT
		// DELETE
	}
}

func (s *discoveryRouter) getDiscovery(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return nil
}

func (s *discoveryRouter) postDiscoveryCreate(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return nil
}
