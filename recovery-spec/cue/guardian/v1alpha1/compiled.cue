package v1alpha1

#ResourceIdentity: close({
	apiVersion: #APIVersion
	kind:       #Kind
	name:       #Name
})

#CompiledResource: close({
	identity:    #ResourceIdentity
	generation?: int & >=0
	specHash:    #SpecHash
	spec:        _
})

#CompiledSpecification: close({
	apiVersion: "guardian.verself.sh/v1alpha1"
	kind:       "CompiledSpecification"
	specHash:   #SpecHash
	resources: [...#CompiledResource]
})
