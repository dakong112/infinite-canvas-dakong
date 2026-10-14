"use client";

import { Suspense } from "react";

import { EcommerceSetWorkbench } from "../components/ecommerce-set-workbench";

export default function EcommerceDetailPage() {
    return (
        <Suspense>
            <EcommerceSetWorkbench preset="detail" />
        </Suspense>
    );
}
