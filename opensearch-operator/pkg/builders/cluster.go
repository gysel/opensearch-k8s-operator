package builders

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	opsterv1 "opensearch.opster.io/api/v1"
	"opensearch.opster.io/pkg/helpers"
)

/// package that declare and build all the resources that related to the OpenSearch cluster ///

func NewSTSForNodePool(cr *opsterv1.OpenSearchCluster, node opsterv1.NodePool, volumes []corev1.Volume, volumeMounts []corev1.VolumeMount) *appsv1.StatefulSet {
	disk := fmt.Sprint(node.DiskSize)

	availableRoles := []string{
		"master",
		"data",
		//"data_content",
		//"data_hot",
		//"data_warm",:
		//"data_cold",
		//"data_frozen",
		"ingest",
		//"ml",
		//"remote_cluster_client",
		//"transform",
	}
	var selectedRoles []string
	for _, role := range node.Roles {
		if helpers.ContainsString(availableRoles, role) {
			selectedRoles = append(selectedRoles, role)
		}
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(disk),
				},
			},
		},
	}
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "pvc",
		MountPath: "/usr/share/opensearch/data",
	})

	clusterInitNode := helpers.CreateInitMasters(cr)
	//var vendor string
	labels := map[string]string{
		"opensearch.cluster":  cr.Name,
		"opensearch.nodepool": node.Component,
	}
	if helpers.ContainsString(selectedRoles, "master") {
		labels["opensearch.role"] = "master"
	}
	runas := int64(0)

	if cr.Spec.General.Vendor == "Op" || cr.Spec.General.Vendor == "OP" ||
		cr.Spec.General.Vendor == "Opensearch" ||
		cr.Spec.General.Vendor == "opensearch" ||
		cr.Spec.General.Vendor == "" {
		//	vendor = "opensearchproject/opensearch"
	} else {
		panic("vendor=elasticsearch not implemented")
		//vendor ="elasticsearch"
	}

	var jvm string
	if node.Jvm == "" {
		jvm = "-Xmx512M -Xms512M"
	} else {
		jvm = node.Jvm
	}
	// Supress repeated log messages about a deprecated format for the publish address
	jvm += " -Dopensearch.transport.cname_in_publish_address=true"

	probe := corev1.Probe{
		PeriodSeconds:       20,
		TimeoutSeconds:      5,
		FailureThreshold:    10,
		SuccessThreshold:    1,
		InitialDelaySeconds: 10,
		ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.IntOrString{IntVal: cr.Spec.General.HttpPort}}},
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-" + node.Component,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &node.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			PodManagementPolicy: "Parallel",
			UpdateStrategy:      appsv1.StatefulSetUpdateStrategy{Type: "RollingUpdate"},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: nil,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Env: []corev1.EnvVar{
								{
									Name:  "cluster.initial_master_nodes",
									Value: clusterInitNode,
								},
								{
									Name:  "discovery.seed_hosts",
									Value: cr.Spec.General.ServiceName,
								},
								{
									Name:  "cluster.name",
									Value: cr.Name,
								},
								{
									Name:  "network.bind_host",
									Value: "0.0.0.0",
								},
								{
									// Make elasticsearch announce its hostname instead of IP so that certificates using the hostname can be verified
									Name:      "network.publish_host",
									ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"}},
								},
								{
									Name:  "OPENSEARCH_JAVA_OPTS",
									Value: jvm,
								},
								{
									Name:  "node.roles",
									Value: strings.Join(selectedRoles, ","),
								},
							},

							Name:  cr.Name,
							Image: DockerImageForCluster(cr),
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: cr.Spec.General.HttpPort,
								},
								{
									Name:          "transport",
									ContainerPort: 9300,
								},
							},
							StartupProbe:  &probe,
							LivenessProbe: &probe,
							VolumeMounts:  volumeMounts,
						},
					},
					InitContainers: []corev1.Container{{
						Name:    "init",
						Image:   "busybox",
						Command: []string{"sh", "-c"},
						Args:    []string{"chown -R 1000:1000 /usr/share/opensearch/data"},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: &runas,
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "pvc",
								MountPath: "/usr/share/opensearch/data",
							},
						},
					},
					},
					Volumes: volumes,
					//NodeSelector:       nil,
					ServiceAccountName: cr.Spec.General.ServiceAccount,
					//	Affinity:           nil,
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{pvc},
			ServiceName:          cr.Spec.General.ServiceName,
		},
	}
}

func DockerImageForCluster(cr *opsterv1.OpenSearchCluster) string {
	// TODO: Determine version based on CR
	return "opensearchproject/opensearch:1.2.3"
}

func NewHeadlessServiceForNodePool(cr *opsterv1.OpenSearchCluster, nodePool *opsterv1.NodePool) *corev1.Service {

	labels := map[string]string{
		"opensearch.cluster":  cr.Name,
		"opensearch.nodepool": nodePool.Component,
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", cr.Spec.General.ServiceName, nodePool.Component),
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: "TCP",
					Port:     cr.Spec.General.HttpPort,
					TargetPort: intstr.IntOrString{
						IntVal: cr.Spec.General.HttpPort,
					},
				},
				{
					Name:     "transport",
					Protocol: "TCP",
					Port:     9300,
					TargetPort: intstr.IntOrString{
						IntVal: 9300,
						StrVal: "9300",
					},
				},
			},
			Selector: labels,
			Type:     "",
		},
	}
}

func NewServiceForCR(cr *opsterv1.OpenSearchCluster) *corev1.Service {

	labels := map[string]string{
		"opensearch.cluster": cr.Name,
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.General.ServiceName,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: "TCP",
					Port:     cr.Spec.General.HttpPort,
					TargetPort: intstr.IntOrString{
						IntVal: cr.Spec.General.HttpPort,
					},
				},
				{
					Name:     "transport",
					Protocol: "TCP",
					Port:     9300,
					TargetPort: intstr.IntOrString{
						IntVal: 9300,
						StrVal: "9300",
					},
				},
				{
					Name:     "metrics",
					Protocol: "TCP",
					Port:     9600,
					TargetPort: intstr.IntOrString{
						IntVal: 9600,
						StrVal: "9600",
					},
				},
				{
					Name:     "rca",
					Protocol: "TCP",
					Port:     9650,
					TargetPort: intstr.IntOrString{
						IntVal: 9650,
						StrVal: "9650",
					},
				},
			},
			Selector: labels,
			Type:     "",
		},
	}
}

func NewNodePortService(cr *opsterv1.OpenSearchCluster) *corev1.Service {
	labels := map[string]string{
		"opensearch.cluster": cr.Name,
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.General.ServiceName + "-exposed",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: "TCP",
					Port:     cr.Spec.General.HttpPort,
					TargetPort: intstr.IntOrString{
						IntVal: cr.Spec.General.HttpPort,
					},
				},
			},
			Selector: labels,
			Type:     "NodePort",
		},
	}
}
func PortForCluster(cr *opsterv1.OpenSearchCluster) int32 {
	httpPort := int32(9200)
	if cr.Spec.General.HttpPort > 0 {
		httpPort = cr.Spec.General.HttpPort
	}
	return httpPort
}
func URLForCluster(cr *opsterv1.OpenSearchCluster) string {
	httpPort := PortForCluster(cr)
	return fmt.Sprintf("https://%s.svc.cluster.local:%d", DnsOfService(cr), httpPort)
	//return fmt.Sprintf("https://localhost:9212")
}

func DnsOfService(cr *opsterv1.OpenSearchCluster) string {
	return fmt.Sprintf("%s.%s", cr.Spec.General.ServiceName, cr.Namespace)
}

func StsName(cr *opsterv1.OpenSearchCluster, nodePool *opsterv1.NodePool) string {
	return cr.Name + "-" + nodePool.Component
}

func ReplicaHostName(currentSts appsv1.StatefulSet, repNum int32) string {
	return fmt.Sprintf("%s-%d", currentSts.ObjectMeta.Name, repNum)
}
