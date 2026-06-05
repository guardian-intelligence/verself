package v1alpha1

#APIVersion: =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/v[0-9]+((alpha|beta)[0-9]+)?$"
#Kind:       =~"^[A-Z][A-Za-z0-9]*$"
#Name:       =~"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
#SpecHash:   =~"^sha256:[a-f0-9]{64}$"

#Metadata: close({
	name:        #Name
	generation?: int & >=0
})

#Resource: close({
	apiVersion: #APIVersion
	kind:       #Kind
	metadata:   #Metadata
	spec:        _
})

#Report: close({
	apiVersion: #APIVersion
	kind:       #Kind
	metadata:   #Metadata
	status:      #ReportStatus
})

#ReportStatus: close({
	observedGeneration: int & >=0
	specHash:           #SpecHash
	conditions: [#Condition, ...#Condition]
})

#Condition: close({
	type:               =~"^[A-Z][A-Za-z0-9]*$"
	status:             "True" | "False" | "Unknown"
	reason:             =~"^[A-Z][A-Za-z0-9]*$"
	message?:           string
	observedGeneration: int & >=0
	lastTransitionTime: =~"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$"
})
