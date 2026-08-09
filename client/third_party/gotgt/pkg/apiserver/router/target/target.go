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
package target

import (
	"net/http"

	"github.com/gostor/gotgt/pkg/apiserver/httputils"
	"github.com/gostor/gotgt/pkg/apiserver/router"
	"github.com/gostor/gotgt/pkg/scsi"
	"golang.org/x/net/context"
)

// containerRouter is a router to talk with the container controller
type targetRouter struct {
	routes []router.Route
}

// NewRouter initializes a new container router
func NewRouter() router.Router {
	r := &targetRouter{}
	r.initRoutes()
	return r
}

// Routes returns the available routers to the container controller
func (r *targetRouter) Routes() []router.Route {
	return r.routes
}

// initRoutes initializes the routes in target router
func (r *targetRouter) initRoutes() {
	r.routes = []router.Route{
		// GET
		router.NewGetRoute("/target/list", r.getTargetList),
		// POST
		router.NewPostRoute("/target/create", r.postTargetCreate),
		router.NewPostRoute("/target/up", r.postTargetUp),
		// PUT
		// DELETE
		router.NewDeleteRoute("/target/{name:.*}", r.deleteTarget),
	}
}

func (r *targetRouter) getTargetList(ctx context.Context, w http.ResponseWriter, req *http.Request, vars map[string]string) error {
	service := scsi.NewSCSITargetService()
	tgts, err := service.GetTargetList()
	if err != nil {
		return err
	}
	return httputils.WriteJSON(w, http.StatusOK, tgts)
}

func (r *targetRouter) postTargetCreate(ctx context.Context, w http.ResponseWriter, req *http.Request, vars map[string]string) error {
	return nil
}

func (r *targetRouter) postTargetUp(ctx context.Context, w http.ResponseWriter, req *http.Request, vars map[string]string) error {
	return nil
}

func (r *targetRouter) deleteTarget(ctx context.Context, w http.ResponseWriter, req *http.Request, vars map[string]string) error {
	return nil
}
