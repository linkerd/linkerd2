package k8s

import (
	"testing"

	serverv1beta3 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestServerIncludesPod(t *testing.T) {
	podWithPorts := func(labels map[string]string, ports ...corev1.ContainerPort) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "emojivoto", Labels: labels},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "web", Ports: ports}},
			},
		}
	}
	serverWithPort := func(port intstr.IntOrString) serverv1beta3.Server {
		return serverv1beta3.Server{
			ObjectMeta: metav1.ObjectMeta{Name: "web-http", Namespace: "emojivoto"},
			Spec: serverv1beta3.ServerSpec{
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
				Port:        port,
			},
		}
	}
	appWeb := map[string]string{"app": "web"}

	tests := []struct {
		name   string
		server serverv1beta3.Server
		pod    corev1.Pod
		want   bool
	}{
		{
			name:   "numeric port matches the pod's unnamed container port",
			server: serverWithPort(intstr.FromInt(8080)),
			pod:    podWithPorts(appWeb, corev1.ContainerPort{ContainerPort: 8080}),
			want:   true,
		},
		{
			name:   "numeric port matches the pod's named container port",
			server: serverWithPort(intstr.FromInt(8080)),
			pod:    podWithPorts(appWeb, corev1.ContainerPort{Name: "http", ContainerPort: 8080}),
			want:   true,
		},
		{
			name:   "numeric port is not exposed and the pod only has an unnamed port",
			server: serverWithPort(intstr.FromInt(8080)),
			pod:    podWithPorts(appWeb, corev1.ContainerPort{ContainerPort: 9090}),
			want:   false,
		},
		{
			name:   "numeric port is not exposed and the pod only has a named port",
			server: serverWithPort(intstr.FromInt(8080)),
			pod:    podWithPorts(appWeb, corev1.ContainerPort{Name: "grpc", ContainerPort: 9090}),
			want:   false,
		},
		{
			name:   "numeric port matches one of several container ports",
			server: serverWithPort(intstr.FromInt(8080)),
			pod: podWithPorts(appWeb,
				corev1.ContainerPort{ContainerPort: 9090},
				corev1.ContainerPort{ContainerPort: 8080},
			),
			want: true,
		},
		{
			name:   "named port matches the pod's port name",
			server: serverWithPort(intstr.FromString("http")),
			pod:    podWithPorts(appWeb, corev1.ContainerPort{Name: "http", ContainerPort: 8080}),
			want:   true,
		},
		{
			name:   "named port does not match another port name",
			server: serverWithPort(intstr.FromString("http")),
			pod:    podWithPorts(appWeb, corev1.ContainerPort{Name: "grpc", ContainerPort: 9090}),
			want:   false,
		},
		{
			name:   "named port does not match an unnamed port",
			server: serverWithPort(intstr.FromString("http")),
			pod:    podWithPorts(appWeb, corev1.ContainerPort{ContainerPort: 8080}),
			want:   false,
		},
		{
			name:   "pod without container ports",
			server: serverWithPort(intstr.FromInt(8080)),
			pod:    podWithPorts(appWeb),
			want:   false,
		},
		{
			name:   "pod labels do not match the selector",
			server: serverWithPort(intstr.FromInt(8080)),
			pod:    podWithPorts(map[string]string{"app": "other"}, corev1.ContainerPort{ContainerPort: 8080}),
			want:   false,
		},
		{
			name: "server without a pod selector",
			server: serverv1beta3.Server{
				Spec: serverv1beta3.ServerSpec{Port: intstr.FromInt(8080)},
			},
			pod:  podWithPorts(appWeb, corev1.ContainerPort{ContainerPort: 8080}),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := serverIncludesPod(tc.server, []corev1.Pod{tc.pod})
			if got != tc.want {
				t.Errorf("serverIncludesPod() = %t, want %t", got, tc.want)
			}
		})
	}
}
