package kubernetes_test

import (
	"context"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"github.com/neilotoole/slogt"
	"github.com/sablierapp/sablier/app/instance"
	"github.com/sablierapp/sablier/config"
	"github.com/sablierapp/sablier/pkg/provider/kubernetes"
	"gotest.tools/v3/assert"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestKubernetesProvider_DeploymentInspect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	ctx := context.Background()
	type args struct {
		do func(dind *kindContainer) (string, error)
	}
	tests := []struct {
		name    string
		args    args
		want    instance.State
		wantErr error
	}{
		{
			name: "deployment with 1/1 replicas",
			args: args{
				do: func(dind *kindContainer) (string, error) {
					d, err := dind.CreateMimicDeployment(ctx, MimicOptions{
						Cmd:         []string{"/mimic"},
						Healthcheck: nil,
					})
					if err != nil {
						return "", err
					}

					if err = WaitForDeploymentReady(ctx, dind.client, "default", d.Name); err != nil {
						return "", fmt.Errorf("error waiting for deployment: %w", err)
					}

					return kubernetes.DeploymentName(d, kubernetes.ParseOptions{Delimiter: "_"}).Original, nil
				},
			},
			want: instance.State{
				CurrentReplicas: 1,
				DesiredReplicas: 1,
				Status:          instance.Ready,
			},
			wantErr: nil,
		},
		{
			name: "deployment with 0/1 replicas",
			args: args{
				do: func(dind *kindContainer) (string, error) {
					d, err := dind.CreateMimicDeployment(ctx, MimicOptions{
						Cmd:         []string{"/mimic", "-running-after=1ms", "-healthy=false", "-healthy-after=10s"},
						Healthcheck: &corev1.Probe{},
					})
					if err != nil {
						return "", err
					}

					return kubernetes.DeploymentName(d, kubernetes.ParseOptions{Delimiter: "_"}).Original, nil
				},
			},
			want: instance.State{
				CurrentReplicas: 0,
				DesiredReplicas: 1,
				Status:          instance.NotReady,
			},
			wantErr: nil,
		},
		{
			name: "deployment with 0/0 replicas",
			args: args{
				do: func(dind *kindContainer) (string, error) {
					d, err := dind.CreateMimicDeployment(ctx, MimicOptions{})
					if err != nil {
						return "", err
					}

					_, err = dind.client.AppsV1().Deployments(d.Namespace).UpdateScale(ctx, d.Name, &autoscalingv1.Scale{
						ObjectMeta: metav1.ObjectMeta{
							Name: d.Name,
						},
						Spec: autoscalingv1.ScaleSpec{
							Replicas: 0,
						},
					}, metav1.UpdateOptions{})
					if err != nil {
						return "", err
					}

					if err = WaitForDeploymentScale(ctx, dind.client, "default", d.Name, 0); err != nil {
						return "", fmt.Errorf("error waiting for deployment: %w", err)
					}

					return kubernetes.DeploymentName(d, kubernetes.ParseOptions{Delimiter: "_"}).Original, nil
				},
			},
			want: instance.State{
				CurrentReplicas: 0,
				DesiredReplicas: 1,
				Status:          instance.NotReady,
			},
			wantErr: nil,
		},
	}
	c := setupKinD(t, ctx)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := kubernetes.NewKubernetesProvider(ctx, c.client, slogt.New(t), config.NewProviderConfig().Kubernetes)

			name, err := tt.args.do(c)
			assert.NilError(t, err)

			tt.want.Name = name
			got, err := p.InstanceInspect(ctx, name)
			if !cmp.Equal(err, tt.wantErr) {
				t.Errorf("KubernetesProvider.InstanceInspect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.DeepEqual(t, got, tt.want)
		})
	}
}
