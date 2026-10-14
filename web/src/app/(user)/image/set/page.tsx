"use client";

import { Suspense } from "react";

import { EcommerceSetWorkbench } from "../components/ecommerce-set-workbench";

export default function EcommerceSetPage() {
    return (
        <Suspense>
            <EcommerceSetWorkbench preset="set" />
        </Suspense>
    );
}
