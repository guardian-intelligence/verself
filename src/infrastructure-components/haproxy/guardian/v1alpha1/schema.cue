package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#DNSName:        =~"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$"
#CIDR:           =~"^[0-9a-fA-F:.]+/[0-9]{1,3}$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#HAProxyGateway: {
	apiVersion: "haproxy.guardianintelligence.org/v1alpha1"
	kind:       "HAProxyGateway"
	metadata:   #Metadata
	spec: {
		runtimeRoot:   #AbsolutePath
		configPath:    #AbsolutePath
		upstreamsPath: #AbsolutePath
		origins: [#ObjectRef & {
			apiVersion: "networking.guardianintelligence.org/v1alpha1"
			kind:       "PublicOrigin"
		}, ...]
		certificates: [...{
			name:     #NonEmptyString
			dnsNames: [#DNSName, ...#DNSName]
			pemPath:  #AbsolutePath
		}]
		routes: [...{
			name:      #NonEmptyString
			originRef: #ObjectRef & {
				apiVersion: "networking.guardianintelligence.org/v1alpha1"
				kind:       "PublicOrigin"
			}
			hostname:      #DNSName
			backend:       #NonEmptyString
			paths?:        [...#AbsolutePath]
			pathPrefixes?: [...#AbsolutePath]
		}]
		cloudflare?: {
			trustedCIDRs: [...#CIDR]
		}
		readiness?: {
			paths: [...#NonEmptyString]
		}
	}
}
