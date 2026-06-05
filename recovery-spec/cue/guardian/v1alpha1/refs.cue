package v1alpha1

#LocalRef: #Name

#ProviderRef:  #LocalRef
#AuthorityRef: #LocalRef
#DomainRef:    #LocalRef
#SecretRef:    #LocalRef

#AllowedValueFrom: "environment.ingress.publicIPv4"

#RefFields: close({
	providerRef?:  #ProviderRef
	authorityRef?: #AuthorityRef
	domainRef?:    #DomainRef
	secretRef?:    #SecretRef
	valueFrom?:    #AllowedValueFrom
})
