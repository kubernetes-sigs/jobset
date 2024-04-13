from __future__ import print_function
from kubernetes import client, config
from kubernetes.client.rest import ApiException
from pprint import pprint
import jobset
from kubernetes.client.models.v1_job_template_spec import V1JobTemplateSpec
from kubernetes.client.models.v1_object_meta import V1ObjectMeta
from kubernetes.client.models.v1_pod_template_spec import V1PodTemplateSpec


config.load_kube_config()
# Enter a context with an instance of the API kubernetes.client
with client.ApiClient() as api_client:
    # Create an instance of the API class
    api_instance = client.CustomObjectsApi(api_client)
    group = "jobset.x-k8s.io"  # str | the custom resource's group
version = "v1"  # str | the custom resource's version
namespace = "default"  # str | The custom resource's namespace
plural = "jobsets"  # str | the custom resource's plural name. For TPRs this would be lowercase plural kind.
name = "failurepolicy"  # str | the custom object's name
container = client.V1Container(
    name="pi", image="perl", command=["perl", "-Mbignum=bpi", "-wle", "print bpi(2000)"]
)
job_template = client.V1JobTemplateSpec()
job_template.spec = client.V1JobSpec(
    template=client.V1PodTemplateSpec(
        spec=client.V1PodSpec(restart_policy="Never", containers=[container])
    )
)
replicated_job = (
    jobset.models.jobset_v1_replicated_job.JobsetV1ReplicatedJob(
        name="main",
        replicas=1,
        template=job_template,
    )
)
jobset_example = jobset.models.jobset_v1_job_set.JobsetV1JobSet(
    api_version="jobset.x-k8s.io/v1",
    kind="JobSet",
    metadata=V1ObjectMeta(name="test-jobset"),
    spec=jobset.models.jobset_v1_job_set_spec.JobsetV1JobSetSpec(
        replicated_jobs=[replicated_job],
        suspend=True,
    ),
)

# Create a jobset
try:
    api_response = api_instance.create_namespaced_custom_object(
        group, version, namespace, plural, jobset_example
    )
    pprint(api_response)
except ApiException as e:
    print(
        "Exception when calling CustomObjectsApi->create_namespaced_custom_object: %s\n"
        % e
    )

# List jobsets
try:
    api_response = api_instance.list_namespaced_custom_object(
        group, version, namespace, plural
    )
    pprint(api_response)
except ApiException as e:
    print(
        "Exception when calling CustomObjectsApi->list_namespaced_custom_object: %s\n"
        % e
    )

# Get a jobset
try:
    api_response = api_instance.get_namespaced_custom_object(
        group, version, namespace, plural, "test-jobset"
    )
    pprint(api_response)
except ApiException as e:
    print(
        "Exception when calling CustomObjectsApi->get_namespaced_custom_object: %s\n"
        % e
    )


