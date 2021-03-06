package k8s_helpers

import (
	"os/exec"
	"encoding/json"

	route_crds "code.cloudfoundry.org/cf-k8s-networking/routecontroller/api/v1alpha1"
)

func Kubectl(args ...string) ([]byte, error) {
	cmd := exec.Command("kubectl", args...)

	output, err := cmd.CombinedOutput()
	return output, err
}

func KubectlGetRoute(namespace, routeGuid string) (route_crds.Route, error) {
	var route route_crds.Route

	output, err := Kubectl("get", "route", routeGuid, "-n", namespace, "-o", "json")
	if err != nil {
		return route_crds.Route{}, err
	}

	err = json.Unmarshal(output, &route)
	if err != nil {
		return route_crds.Route{}, err
	}
	return route, nil
}
