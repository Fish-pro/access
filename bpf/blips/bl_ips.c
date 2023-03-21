//go:build ignore

#include "common.h"
#include "bpf_endian.h"

char __license[] SEC("license") = "Dual MIT/GPL";

struct bpf_map_def SEC("maps") blacklist = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(u32),
    .value_size = sizeof(u8),
    .max_entries = 100000,
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

SEC("xdp")
int drop_bl_arp(struct xdp_md *ctx) {
    void *data_end = (void *)(long)ctx->data_end;
    void *data = (void *)(long)ctx->data;
    u32 ip_src;
    u64 *value;
    struct ethhdr *eth = data;

    if (eth->h_proto != bpf_htons(0x0806)) {
        return XDP_PASS;
    }

    struct arp_t *arp = data + sizeof(*eth);

    ip_src = arp->tpa;
    value = bpf_map_lookup_elem(&blacklist, &ip_src);
    if (value) {
        return XDP_DROP;
    }

    return XDP_PASS;
}
