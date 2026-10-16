"use client";

import { DeleteOutlined, UploadOutlined } from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Flex, Input, Popconfirm, Space, Table, Tag, Typography } from "antd";
import { useRef, useState } from "react";

import { formatSeconds } from "@/lib/product-video";
import { addBgmTrack, deleteBgmTrack, listBgmTracks, type BgmTrack } from "@/services/api/product-video";
import { uploadMediaBlob } from "@/services/file-storage";
import { useUserStore } from "@/stores/use-user-store";

const MAX_BYTES = 20 * 1024 * 1024;

/** Duration the browser reads from the file; the server replaces it with ffprobe's when installed. */
const audioSeconds = (file: File) =>
    new Promise<number>((resolve) => {
        const audio = new Audio();
        const url = URL.createObjectURL(file);
        const done = (seconds: number) => {
            URL.revokeObjectURL(url);
            resolve(Number.isFinite(seconds) ? seconds : 0);
        };
        audio.onloadedmetadata = () => done(audio.duration);
        audio.onerror = () => done(0);
        audio.src = url;
    });

/** "背景音乐库": tracks users pick in the product video wizard; uploaded files become public. */
export function BgmLibrarySection() {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const token = useUserStore((state) => state.token);
    const inputRef = useRef<HTMLInputElement>(null);
    const [name, setName] = useState("");
    const [tags, setTags] = useState("");
    const [busy, setBusy] = useState(false);
    const { data: tracks = [], isLoading } = useQuery({ queryKey: ["bgm-tracks"], queryFn: listBgmTracks });
    const setTracks = (next: BgmTrack[]) => queryClient.setQueryData(["bgm-tracks"], next);

    const upload = async (file?: File) => {
        if (!file || !token) return;
        if (!file.type.startsWith("audio/") || file.size > MAX_BYTES) return void message.warning("请选择 20MB 以内的音频文件");
        setBusy(true);
        try {
            const [{ storageKey }, durationSec] = await Promise.all([uploadMediaBlob(file, file.name), audioSeconds(file)]);
            if (!storageKey.startsWith("server:") || storageKey.startsWith("server:webdav:")) throw new Error("服务端存储未启用，无法上传曲目");
            const trackName = name.trim() || file.name.replace(/\.[^.]+$/, "");
            setTracks(await addBgmTrack(token, { fileId: storageKey.slice("server:".length), name: trackName, tags: tags.split(/[,，\s]+/).filter(Boolean), durationSec }));
            setName("");
            setTags("");
            message.success(`已添加「${trackName}」`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "上传失败");
        } finally {
            setBusy(false);
        }
    };

    return (
        <Flex vertical gap={8} className="mb-6">
            <Typography.Title level={5} className="!mb-0">
                背景音乐库
            </Typography.Title>
            <Typography.Paragraph type="secondary" className="!mb-0 !text-xs">
                带货视频“背景音乐”下拉中的曲目，上传后文件公开；请只上传有版权或 CC0 / 公有领域的音乐。增删立即生效，无需保存设置。
            </Typography.Paragraph>
            <Space.Compact className="w-full max-w-xl">
                <Input placeholder="曲目名称（留空用文件名）" maxLength={50} value={name} onChange={(event) => setName(event.target.value)} />
                <Input placeholder="风格标签，逗号分隔，如 欢快,电子" value={tags} onChange={(event) => setTags(event.target.value)} />
                <Button icon={<UploadOutlined />} loading={busy} onClick={() => inputRef.current?.click()}>
                    上传音频
                </Button>
            </Space.Compact>
            <input ref={inputRef} type="file" accept="audio/*" hidden onChange={(event) => void upload(event.target.files?.[0]).finally(() => (event.target.value = ""))} />
            <Table<BgmTrack>
                size="small"
                rowKey="fileId"
                loading={isLoading}
                pagination={false}
                dataSource={tracks}
                locale={{ emptyText: "曲库为空" }}
                columns={[
                    { title: "名称", dataIndex: "name" },
                    { title: "风格", dataIndex: "tags", render: (value: BgmTrack["tags"]) => (value ?? []).map((tag) => <Tag key={tag}>{tag}</Tag>) },
                    { title: "时长", dataIndex: "durationSec", width: 80, render: (value: number) => formatSeconds(value) },
                    { title: "试听", dataIndex: "url", render: (url: string) => <audio src={url} controls preload="none" className="h-8" /> },
                    {
                        title: "",
                        width: 48,
                        render: (_, track) => (
                            <Popconfirm title="从曲库移除？已使用它的视频不受影响" onConfirm={async () => token && setTracks(await deleteBgmTrack(token, track.fileId))}>
                                <Button type="text" danger size="small" icon={<DeleteOutlined />} aria-label="移除曲目" />
                            </Popconfirm>
                        ),
                    },
                ]}
            />
        </Flex>
    );
}
