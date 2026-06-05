package v1alpha1

#APIVersion: =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/v[0-9]+((alpha|beta)[0-9]+)?$"
#Kind:       =~"^[A-Z][A-Za-z0-9]*$"
#Name:       =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"

#Metadata: close({
	name: #Name
})

#Resource: close({
	apiVersion: #APIVersion
	kind:       #Kind
	metadata:   #Metadata
	spec:        _
})
