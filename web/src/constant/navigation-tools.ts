import { FolderOpen, Home, ImagePlus, Images, LayoutGrid, LibraryBig, Video } from "lucide-react";

export const navigationTools = [
    { key: "home", href: "/", label: "首页", icon: Home },
    { key: "image", href: "/image", label: "图片制作", icon: ImagePlus },
    { key: "video", href: "/video", label: "视频制作", icon: Video },
    { key: "canvas", href: "/canvas", label: "无限画布", icon: LayoutGrid },
    { key: "prompts", href: "/prompts", label: "模板库", icon: LibraryBig },
    { key: "assets", href: "/assets", label: "我的资产", icon: FolderOpen },
    { key: "works", href: "/works", label: "我的作品", icon: Images },
] as const;

export type NavigationToolKey = (typeof navigationTools)[number]["key"];

export function isNavigationActive(pathname: string, href: string) {
    if (href === "/") return pathname === "/";
    return pathname === href || pathname.startsWith(`${href}/`);
}
