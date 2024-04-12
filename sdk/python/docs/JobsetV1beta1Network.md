# JobsetV1beta1Network

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**enable_dns_hostnames** | **bool** | EnableDNSHostnames allows pods to be reached via their hostnames. Pods will be reachable using the fully qualified pod hostname: &lt;jobSet.name&gt;-&lt;spec.replicatedJob.name&gt;-&lt;job-index&gt;-&lt;pod-index&gt;.&lt;subdomain&gt; | [optional] 
**subdomain** | **str** | Subdomain is an explicit choice for a network subdomain name When set, any replicated job in the set is added to this network. Defaults to &lt;jobSet.name&gt; if not set. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


