"use client";

import { useQuery } from "@tanstack/react-query";

import { getNewapiAuthConfig } from "@/services/api/newapi-auth";

/** SSO config (incl. the new-api base URL); cached for an hour. */
export function useNewapiConfig(enabled = true) {
    return useQuery({ queryKey: ["newapi-auth-config"], queryFn: getNewapiAuthConfig, staleTime: 60 * 60 * 1000, enabled });
}
