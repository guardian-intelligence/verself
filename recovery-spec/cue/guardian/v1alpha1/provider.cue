package v1alpha1

#ProviderConfig: close({
	apiVersion: #APIVersion
	kind:       #Kind
	spec:       _
})

#ProviderBinding: close({
	apiVersion: "guardian.verself.sh/v1alpha1"
	kind:       "ProviderBinding"
	metadata:   #Metadata
	spec: close({
		provider:       #Name
		authorityRef:   #AuthorityRef
		providerConfig: #ProviderConfig
	})
})
