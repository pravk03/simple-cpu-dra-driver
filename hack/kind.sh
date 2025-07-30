#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) SchedMD LLC.
# SPDX-License-Identifier: Apache-2.0

# https://kind.sigs.k8s.io/docs/user/quick-start/

set -euo pipefail

SCRIPT_DIR="$(readlink -f "$(dirname "$0")")"

function kind::prerequisites() {
	go install sigs.k8s.io/kind@latest
}

function kind::start() {
	kind::prerequisites
	local cluster_name="${1:-"kind"}"
	local kind_config="${2:-"$SCRIPT_DIR/kind.yaml"}"
	if [ "$(kind get clusters | grep -oc kind)" -eq 0 ]; then
		if [ "$(command -v systemd-run)" ]; then
			CMD="systemd-run --scope --user"
		else
			CMD=""
		fi
		$CMD kind create cluster --name "$cluster_name" --config "$kind_config"
	fi
	kubectl cluster-info --context kind-"$cluster_name"
}

function kind::delete() {
	kind delete cluster --name "$cluster_name"
}

function cpu_dra::install() {
	kubectl apply -f ../install.yaml
}

function main::help() {
	cat <<EOF
$(basename "$0") - Manage a kind cluster for cpu dra driver

        usage: $(basename "$0") [--create|--delete] [--config=KIND_CONFIG_PATH]
                [-h|--help] [KIND_CLUSTER_NAME]

ONESHOT OPTIONS:
        --install           Install cpu dra driver
        --create            Create kind cluster and nothing else.
        --delete            Delete kind cluster and nothing else.

OPTIONS:
        --config=PATH       Use the specified kind config when creating.

HELP OPTIONS:
        --debug             Show script debug information.
        -h, --help          Show this help message.

EOF
}

function main() {
	if $FLAG_DEBUG; then
		set -x
	fi
	local cluster_name="${1:-"kind"}"
	if $FLAG_DELETE; then
		kind::delete "$cluster_name"
		return
	elif $FLAG_CREATE; then
		kind::start "$cluster_name" "$FLAG_CONFIG"
		return
	fi

	kind::start "$cluster_name" "$FLAG_CONFIG"

	if $FLAG_INSTALL; then
		cpu_dra::install
		return
	fi
}

FLAG_DEBUG=false
FLAG_CREATE=false
FLAG_CONFIG="$SCRIPT_DIR/kind.yaml"
FLAG_DELETE=false
FLAG_INSTALL=false

SHORT="+h"
LONG="create,config:,delete,debug,install,help"
OPTS="$(getopt -a --options "$SHORT" --longoptions "$LONG" -- "$@")"
eval set -- "${OPTS}"
while :; do
	case "$1" in
	--debug)
		FLAG_DEBUG=true
		shift
		;;
	--create)
		FLAG_CREATE=true
		shift
		if $FLAG_CREATE && $FLAG_DELETE; then
			echo "Flags --create and --delete are mutually exclusive!"
			exit 1
		fi
		;;
	--config)
		FLAG_CONFIG="$2"
		shift 2
		;;
	--delete)
		FLAG_DELETE=true
		shift
		if $FLAG_CREATE && $FLAG_DELETE; then
			echo "Flags --create and --delete are mutually exclusive!"
			exit 1
		fi
		;;
	--install)
		FLAG_INSTALL=true
		shift
		;;
	-h | --help)
		main::help
		shift
		exit 0
		;;
	--)
		shift
		break
		;;
	*)
		log::error "Unknown option: $1"
		exit 1
		;;
	esac
done
main "$@"
