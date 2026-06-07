package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#CredentialRef: #ObjectRef & {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "SecretPath"
}

#ProfileService: {
	apiVersion: "profile.guardianintelligence.org/v1alpha1"
	kind:       "ProfileService"
	metadata:   #Metadata
	spec: {
		auth: {
			issuerURL:   #HTTPSURL
			audienceRef: #CredentialRef
		}
		postgres: {
			dsn:      #NonEmptyString
			maxConns: int & >0
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
