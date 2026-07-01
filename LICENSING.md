# IoTMaker Licensing

IoTMaker is **dual-licensed**. You may use it under the open-source license
below, **or** obtain a commercial license.

|                          | Open source                                            | Commercial                                              |
| ------------------------ | ------------------------------------------------------ | ------------------------------------------------------- |
| License                  | GNU AGPL v3.0 (`AGPL-3.0-only`) — see [`LICENSE`](./LICENSE) | Proprietary — contact us                           |
| Cost                     | Free                                                   | Paid                                                    |
| Must you share source?   | Yes — including when run over a network (see below)    | No                                                      |
| For                      | Self-hosters, open-source projects, evaluation, personal use | Companies embedding IoTMaker in a closed product or hosted service |
| Support                  | Community                                              | Included (by agreement)                                 |

## Option 1 — Open source (AGPL-3.0-only)

The full text is in [`LICENSE`](./LICENSE). In plain terms:

- You can run, study, modify and share IoTMaker freely.
- If you distribute a modified version, **or run a modified version as a network
  service**, you must make your complete corresponding source code available to
  your users under the AGPL. This "network clause" is what makes the AGPL
  different from the ordinary GPL, and it is what prevents anyone from taking
  IoTMaker, closing it, and running it as a competing hosted service.
- There is no warranty.

If those terms work for you, you never need to contact us. Just comply with
[`LICENSE`](./LICENSE).

## Option 2 — Commercial license

Buy a commercial license if you want to do something the AGPL does not allow —
for example:

- Embed IoTMaker (or parts of it) in a **proprietary / closed-source** product.
- Offer a **modified, hosted** version of IoTMaker as a service **without**
  publishing your source.
- Any use where AGPL compliance is not acceptable to your organisation.

A commercial license frees you from the AGPL's source-sharing obligations and
can include support and priority fixes.

**Contact:** licensing@iotmaker.io · https://iotmaker.io

> Commercial terms are agreed per customer. This summary is not the contract.

## Your generated code is yours

Code produced by the IoTMaker **generator** (the Go / C99 firmware it outputs)
is **not** covered by the AGPL — even if it embeds IoTMaker runtime or template
code. You own your output and may license it however you like. See
[`GENERATED-CODE-EXCEPTION.md`](./GENERATED-CODE-EXCEPTION.md).

The exception applies to the **output**. The IoTMaker IDE, generator and
libraries themselves remain under the AGPL.

## Contributing

To keep both licensing options possible, every contribution is made under the
Contributor License Agreement in [`CLA.md`](./CLA.md). See
[`CONTRIBUTING.md`](./CONTRIBUTING.md).

## Trademark

"IoTMaker", the IoTMaker logo, and the iotmaker.io domain are **not** licensed
under the AGPL and remain reserved. You may run and fork the code under the
AGPL, but you may not present a fork as the official "IoTMaker" project, or use
the name or logo to imply endorsement. (Registering the mark is recommended —
the AGPL protects the code, not the name.)

---

### README section (copy into your README.md)

```markdown
## License

IoTMaker is dual-licensed under the [GNU AGPLv3](./LICENSE) and a
[commercial license](./LICENSING.md). Code you generate with IoTMaker is
yours — see the [generated-code exception](./GENERATED-CODE-EXCEPTION.md).
Contributions require the [CLA](./CLA.md).
```
