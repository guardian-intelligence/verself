package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#DNSName:        =~"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$"
#IPv4:           =~"^([0-9]{1,3}\\.){3}[0-9]{1,3}$"
#HTTPURL:        =~"^https?://[^\\s]+$"
#Email:          =~"^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$"
#BucketName:     =~"^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#CloudflareControlPlane: {
	apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
	kind:       "CloudflareControlPlane"
	metadata:   #Metadata
	spec: {
		site:       #NonEmptyString
		accountID:  =~"^[a-f0-9]{32}$"
		targetIPv4: #IPv4
		openBao: {
			address:    #HTTPURL
			tokenFile:  #NonEmptyString
			caCertFile: #AbsolutePath
		}
		dns: {
			zones: [...{
				name:   #NonEmptyString
				zone:   #DNSName
				domain: #DNSName
			}]
			records: [...{
				zone:    #NonEmptyString
				record:  #NonEmptyString
				ttl:     int & >0
				proxied: bool
			}]
		}
		tls: {
			outputDir: #AbsolutePath
			acme: {
				directoryURL:       #HTTPURL
				contactEmail:       #Email
				dnsPropagationWait: #NonEmptyString
				renewBefore:        #NonEmptyString
			}
			certificates: [...{
				name: #DNSName
				domains: [#NonEmptyString, ...#NonEmptyString]
			}]
		}
		objectStorage: {
			bucket:        #BucketName
			childTokenTTL: #NonEmptyString
			runtimeSecrets: {
				adminAccessKeyID:     #NonEmptyString
				adminSecretAccessKey: #NonEmptyString
				proxyAccessKeyID:     #NonEmptyString
				proxySecretAccessKey: #NonEmptyString
			}
		}
	}
}
