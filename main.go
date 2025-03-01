/*
Copyright 2020 Red Hat Community of Practice.

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

package main

import (
	"context"
	"flag"
	"os"
	"strconv"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	userv1 "github.com/openshift/api/user/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	redhatcopv1alpha1 "github.com/redhat-cop/namespace-configuration-operator/api/v1alpha1"
	"github.com/redhat-cop/namespace-configuration-operator/controllers"
	"github.com/redhat-cop/operator-utils/pkg/util/discoveryclient"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller"
	// +kubebuilder:scaffold:imports
)

const (
	AllowSystemNamespacesEnvVarKey = "ALLOW_SYSTEM_NAMESPACES"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(redhatcopv1alpha1.AddToScheme(scheme))
	utilruntime.Must(userv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                     scheme,
		MetricsBindAddress:         metricsAddr,
		Port:                       9443,
		HealthProbeBindAddress:     probeAddr,
		LeaderElection:             enableLeaderElection,
		LeaderElectionID:           "b0b2f089.redhat.io",
		LeaderElectionResourceLock: "configmaps",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.NamespaceConfigReconciler{
		EnforcingReconciler:   lockedresourcecontroller.NewEnforcingReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetAPIReader(), mgr.GetEventRecorderFor("NamespaceConfig_controller"), true, true),
		Log:                   ctrl.Log.WithName("controllers").WithName("NamespaceConfig"),
		AllowSystemNamespaces: checkNamespaceScope(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NamespaceConfig")
		os.Exit(1)
	}

	userConfigController := &controllers.UserConfigReconciler{
		EnforcingReconciler: lockedresourcecontroller.NewEnforcingReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetAPIReader(), mgr.GetEventRecorderFor("UserConfig_controller"), true, true),
		Log:                 ctrl.Log.WithName("controllers").WithName("UserConfig"),
	}

	if ok, err := discoveryclient.IsGVKDefined(context.TODO(), schema.GroupVersionKind{
		Group:   "user.openshift.io",
		Version: "v1",
		Kind:    "User",
	}); !ok || err != nil {
		if err != nil {
			setupLog.Error(err, "unable to set check wheter resource User.user.openshift.io exists")
			os.Exit(1)
		}
	} else {
		if err = (userConfigController).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "UserConfig")
			os.Exit(1)
		}
	}

	groupConfigController := &controllers.GroupConfigReconciler{
		EnforcingReconciler: lockedresourcecontroller.NewEnforcingReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetAPIReader(), mgr.GetEventRecorderFor("GroupConfig_controller"), true, true),
		Log:                 ctrl.Log.WithName("controllers").WithName("GroupConfig"),
	}

	if ok, err := discoveryclient.IsGVKDefined(context.TODO(), schema.GroupVersionKind{
		Group:   "user.openshift.io",
		Version: "v1",
		Kind:    "Group",
	}); !ok || err != nil {
		if err != nil {
			setupLog.Error(err, "unable to set check wheter resource Group.user.openshift.io exists")
			os.Exit(1)
		}
	} else {
		if err = (groupConfigController).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "GroupConfig")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func checkNamespaceScope() bool {
	value := os.Getenv(AllowSystemNamespacesEnvVarKey)
	if len(value) == 0 {
		return false
	}
	res, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return res
}
