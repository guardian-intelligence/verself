CREATE USER IF NOT EXISTS otelcol IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/otelcol' HOST LOCAL;
ALTER USER otelcol IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/otelcol' HOST LOCAL;
GRANT INSERT ON default.* TO otelcol;
GRANT SELECT ON default.* TO otelcol;
