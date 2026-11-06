package compose

import "strings"

// TraefikOptions configures the Traefik labels of a service.
type TraefikOptions struct {
	Router       string // router and service name; defaults to the service name
	Domain       string
	InternalPort string
	Entrypoint   string // defaults to "web"
	TLS          bool
	CertResolver string
	Middlewares  []string
}

// BuildTraefikLabels keeps the original signature (entrypoint "web", no TLS).
func BuildTraefikLabels(service string, domain string, internalPort string) []string {
	return BuildTraefikLabelsWith(TraefikOptions{
		Router:       service,
		Domain:       domain,
		InternalPort: internalPort,
	})
}

// BuildTraefikLabelsWith renders Traefik v2/v3 docker provider labels.
func BuildTraefikLabelsWith(o TraefikOptions) []string {
	entrypoint := o.Entrypoint
	if entrypoint == "" {
		entrypoint = "web"
	}
	port := o.InternalPort
	if port == "" {
		port = "80"
	}
	r := "traefik.http.routers." + o.Router

	labels := []string{
		"traefik.enable=true",
		r + ".rule=Host(`" + o.Domain + "`)",
		r + ".entrypoints=" + entrypoint,
	}
	if o.TLS {
		labels = append(labels, r+".tls=true")
		if o.CertResolver != "" {
			labels = append(labels, r+".tls.certresolver="+o.CertResolver)
		}
	}
	if len(o.Middlewares) > 0 {
		labels = append(labels, r+".middlewares="+strings.Join(o.Middlewares, ","))
	}
	labels = append(labels,
		"traefik.http.services."+o.Router+".loadbalancer.server.port="+port,
	)
	return labels
}
