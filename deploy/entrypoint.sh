#!/bin/sh
set -e

normalize_base_path() {
	raw="${1:-/}"

	# Trim surrounding spaces
	raw="$(echo "$raw" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"

	if [ -z "$raw" ] || [ "$raw" = "/" ]; then
		echo "/"
		return
	fi

	case "$raw" in
		/*) base="$raw" ;;
		*) base="/$raw" ;;
	esac

	# Remove trailing slash for non-root path
	while [ "$base" != "/" ] && [ "${base%/}" != "$base" ]; do
		base="${base%/}"
	done

	echo "$base"
}

generate_nginx_conf() {
	base_path="$(normalize_base_path "${WEB_BASE_PATH:-/}")"

	if [ "$base_path" = "/" ]; then
		cat > /etc/nginx/conf.d/default.conf <<'EOF'
server {
	listen 80;
	server_name _;

	access_log off;
	error_log /dev/null crit;

	root /usr/share/nginx/html;
	index index.html;

	location /api/ {
		proxy_pass http://localhost:8080;
		proxy_http_version 1.1;
		proxy_set_header Upgrade $http_upgrade;
		proxy_set_header Connection $connection_upgrade;
		proxy_set_header Host $host;
		proxy_set_header X-Real-IP $remote_addr;
		proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;
	}

	location / {
		try_files $uri $uri/ /index.html;
	}
}
EOF
		return
	fi

	api_prefix="${base_path}/api"

	cat > /etc/nginx/conf.d/default.conf <<EOF
server {
	listen 80;
	server_name _;

	access_log off;
	error_log /dev/null crit;

	root /usr/share/nginx/html;
	index index.html;

	location = / {
		return 302 ${base_path}/;
	}

	location = ${base_path} {
		return 301 ${base_path}/;
	}

	location ${api_prefix}/ {
		rewrite ^${base_path}(/api/.*)$ \$1 break;
		proxy_pass http://localhost:8080;
		proxy_http_version 1.1;
		proxy_set_header Upgrade \$http_upgrade;
		proxy_set_header Connection \$connection_upgrade;
		proxy_set_header Host \$host;
		proxy_set_header X-Real-IP \$remote_addr;
		proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto \$scheme;
	}

	location ${base_path}/ {
		rewrite ^${base_path}/?(.*)$ /\$1 break;
		try_files \$uri \$uri/ /index.html;
	}
}
EOF
}

# Needed for websocket upgrade header handling in proxy_set_header Connection.
cat > /etc/nginx/conf.d/00-map.conf <<'EOF'
map $http_upgrade $connection_upgrade {
	default upgrade;
	'' close;
}
EOF

generate_nginx_conf

# Start API in background
nats-console-api &
API_PID=$!

# Start Nginx in foreground
nginx -g "daemon off;"

# If Nginx stops, terminate API
kill $API_PID 2>/dev/null || true
