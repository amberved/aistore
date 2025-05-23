---
layout: post
title: ON DISK LAYOUT
permalink: /docs/on-disk-layout
redirect_from:
 - /on_disk_layout.md/
 - /docs/on_disk_layout.md/
---

AIStore 3.0 introduced new on-disk layout that addressed several motivations including (but not limited to) the motivation to support multiple remote backends.

One of those remote backends can be AIStore itself, with immediate availability of AIS-to-AIS caching and a gamut of related data recovery capabilities.

At a high level:

- in addition to checksum, all metadata (including object metadata) is versioned to provide for **backward compatibility** when (and *if*) there are any future changes;
- cluster-wide control structures -  in particular, cluster map and bucket metadata - are now uniformly GUID-protected and LZ4-compressed;
- bucket metadata is replicated, with multiple protected and versioned copies stored on data drives of **all** storage targets in a cluster.

In addition, AIS supports configurable namespaces whereby users can choose to group selected buckets for the purposes of physical isolation from all other buckets and datasets, and/or applying common (for this group) storage management policies: erasure coding, n-way mirroring, etc. But more about it later.

Here's a simplified drawing depicting two [providers](/docs/providers.md), AIS and AWS, and two buckets, `ABC` and `XYZ`, respectively. In the picture, `mpath` is a single [mountpath](/docs/configuration.md) - a single disk **or** a volume formatted with a local filesystem of choice, **and** a local directory (`mpath/`):

![on-disk hierarchy](/docs/images/PBCT.png)

Further, each bucket would have a unified structure with several system directories (e.g., `%ec` that stores erasure coded content) and, of course, user data under `%ob` ("object") locations.

Needless to say, the same exact structure reproduces itself across all AIS storage nodes, and all data drives of each clustered node.

With namespaces, the picture becomes only slightly more complicated. The following shows two AIS buckets, `DEF` and `GHJ`, under their respective user-defined namespaces called `#namespace-local` and `#namespace-remote`.  Unlike a local namespace of *this* cluster, the remote one would have to be prefixed with UUID - to uniquely identify another AIStore cluster hosting `GHJ` (in this example) and from where this bucket's content will be replicated or cached, on-demand or via Prefetch API and [similar](/docs/overview.md#existing-datasets).

![on-disk hierarchy with namespaces](/docs/images/PBCT-with-namespaces.png)

### References

For the purposes of full disclosure and/or in-depth review, following are initial references into AIS sources that also handle on-disk representation of object metadata:

* [local object metadata (LOM)](https://github.com/NVIDIA/aistore/blob/main/core/lom_xattr.go)

 and AIS control structures:

* [bucket metadata (BMD)](https://github.com/NVIDIA/aistore/blob/main/ais/bucketmeta.go)
* [cluster map (Smap)](https://github.com/NVIDIA/aistore/blob/main/ais/clustermap.go)

## System Files

In addition to user data, AIStore stores, maintains, and utilizes itself a relatively small number of system files that serve a variety of different purposes. Full description of the AIStore *persistence* would not be complete without listing those files (and their respective purposes) - for details, please refer to:

* [System Files](/docs/sysfiles.md)
