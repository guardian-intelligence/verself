package schema

#Host:        string & !=""
#ServiceHost: #Host & !="0.0.0.0" & !="::"
#Port:        int & >=1 & <=65535 & !=4245

#GarageNode: close({
	instance:   int & >=0
	s3_port:    #Port
	rpc_port:   #Port
	admin_port: #Port
})

#Service: close({
	host:         #ServiceHost
	listen_host?: #Host

	port?:                              #Port
	admin_port?:                        #Port
	cluster_port?:                      #Port
	frontend_membership_port?:          #Port
	grpc_port?:                         #Port
	history_grpc_port?:                 #Port
	history_membership_port?:           #Port
	http_port?:                         #Port
	internal_frontend_grpc_port?:       #Port
	internal_frontend_http_port?:       #Port
	internal_frontend_membership_port?: #Port
	internal_port?:                     #Port
	matching_grpc_port?:                #Port
	matching_membership_port?:          #Port
	metrics_port?:                      #Port
	monitoring_port?:                   #Port
	pprof_port?:                        #Port
	secure_native_port?:                #Port
	smtp_port?:                         #Port
	ssh_port?:                          #Port
	statsd_port?:                       #Port
	worker_grpc_port?:                  #Port
	worker_membership_port?:            #Port

	nodes?: [...#GarageNode]
})

#Services: [string]: #Service

#Topology: close({
	services: #Services
})
