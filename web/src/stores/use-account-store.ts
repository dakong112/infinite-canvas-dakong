import { create } from "zustand";

import { fetchAccountSummary, type AccountSummary } from "@/services/api/account";
import { useUserStore } from "@/stores/use-user-store";

type AccountState = {
    summary: AccountSummary | null;
    error: string;
    loading: boolean;
    refresh: () => Promise<void>;
    setSummary: (summary: AccountSummary) => void;
    clear: () => void;
};

export const useAccountStore = create<AccountState>((set) => ({
    summary: null,
    error: "",
    loading: false,
    refresh: async () => {
        const token = useUserStore.getState().token;
        if (!token) {
            set({ summary: null, error: "" });
            return;
        }
        set({ loading: true });
        try {
            set({ summary: await fetchAccountSummary(token), error: "", loading: false });
        } catch (error) {
            set({ error: error instanceof Error ? error.message : "账户信息加载失败", loading: false });
        }
    },
    setSummary: (summary) => set({ summary, error: "" }),
    clear: () => set({ summary: null, error: "" }),
}));
