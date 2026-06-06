package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"
#AbsolutePath:   =~"^/[^\\s]*$"
#BucketName:     =~"^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#CredentialRef: #ObjectRef

#ObjectStorageService: {
	apiVersion: "objectstorage.guardianintelligence.org/v1alpha1"
	kind:       "ObjectStorageService"
	metadata:   #Metadata
	spec: {
		provider: {
			cloudflareR2: {
				endpoint:     #HTTPSURL
				region:       #NonEmptyString
				authorityRef: #ObjectRef
				credentials: {
					adminAccessKeyIDRef:     #CredentialRef
					adminSecretAccessKeyRef: #CredentialRef
					proxyAccessKeyIDRef:     #CredentialRef
					proxySecretAccessKeyRef: #CredentialRef
				}
			}
		}
		buckets: [...{
			name:         #NonEmptyString
			providerName: #BucketName
			purpose: "deploymentArtifacts" | "recovery" | "customerObjects" | "internal"
			quotaBytes?:   int & >0
			quotaObjects?: int & >0
		}]
		auth: {
			issuerURL: #HTTPSURL
			audienceRef: #CredentialRef
		}
		postgres: {
			dsn: #NonEmptyString
		}
		clickhouse: {
			address:    #NonEmptyString
			user:       #NonEmptyString
			caCertPath: #AbsolutePath
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
