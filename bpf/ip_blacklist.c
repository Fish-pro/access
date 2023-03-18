//go:build ignore

#include "common.h"

char __license[] SEC("license") = "Dual MIT/GPL";

struct bpf_elf_map SEC("maps") blacklist = {
    .type = BPF_MAP_TYPE_HASH,
    .size_key = sizeof(u32),
    .size_value = sizeof(u8),
    .max_elem = 100000,
};

struct arp_t {
    unsigned short htype;
    unsigned short ptype;
    unsigned char hlen;
    unsigned char plen;
    unsigned short oper;
    unsigned long long sha:48;
    unsigned long long spa:32;
    unsigned long long tha:48;
    unsigned int tpa;
} __attribute__((packed));

SEC("drop_bl_arp")
int drop_bl_arp(struct xdp_md *ctx) {
    void *data_end = (void *)(long)ctx->data_end;
    void *data = (void *)(long)ctx->data;
    u32 ip_src;
    u64 *value;
    struct ethhdr *eth = data;

    if (eth->h_proto != htons(0x0806)){
        return XDP_PASS;
    }

    struct arp_t *arp = data +sizeof(*eth);

    ip_src = arp->tpa;
    value = bpf_map_lookup_elem(&blacklist, &ip_src);
    if (value) {
        return XDP_DROP;
    }

    return XDP_PASS;
}