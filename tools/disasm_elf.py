#!/usr/bin/env python3
"""Disassemble ARM ELF symbols and inspect direct branch references."""

import argparse

from capstone import CS_ARCH_ARM, CS_MODE_ARM, CS_MODE_LITTLE_ENDIAN, Cs
from elftools.elf.elffile import ELFFile


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("elf")
    parser.add_argument("symbol")
    parser.add_argument("--size", type=lambda value: int(value, 0))
    parser.add_argument(
        "--xrefs",
        action="store_true",
        help="list direct branch instructions that target the named symbol",
    )
    args = parser.parse_args()

    with open(args.elf, "rb") as stream:
        elf = ELFFile(stream)
        symbols = {}
        target = None
        for section_name in (".symtab", ".dynsym"):
            section = elf.get_section_by_name(section_name)
            if section is None:
                continue
            for symbol in section.iter_symbols():
                address = symbol["st_value"]
                if address:
                    symbols.setdefault(address, symbol.name)
                if symbol.name == args.symbol:
                    target = symbol

        if target is None:
            raise SystemExit(f"symbol not found: {args.symbol}")

        address = target["st_value"]
        disassembler = Cs(CS_ARCH_ARM, CS_MODE_ARM | CS_MODE_LITTLE_ENDIAN)
        disassembler.skipdata = True

        if args.xrefs:
            text = elf.get_section_by_name(".text")
            for instruction in disassembler.disasm(text.data(), text["sh_addr"]):
                if instruction.mnemonic not in {"b", "bl", "blx"}:
                    continue
                try:
                    destination = int(instruction.op_str.lstrip("#"), 0)
                except ValueError:
                    continue
                if destination != address:
                    continue
                caller_address = max(
                    (item for item in symbols if item <= instruction.address),
                    default=None,
                )
                caller = symbols.get(caller_address, "unknown")
                print(
                    f"{instruction.address:08x}: {instruction.mnemonic:8s} "
                    f"{instruction.op_str} <{target.name}> from <{caller}>"
                )
            return

        size = args.size or target["st_size"]
        text = elf.get_section_by_name(".text")
        offset = address - text["sh_addr"]
        code = text.data()[offset : offset + size]

        for instruction in disassembler.disasm(code, address):
            annotation = ""
            if instruction.mnemonic in {"b", "bl", "blx"}:
                try:
                    destination = int(instruction.op_str.lstrip("#"), 0)
                except ValueError:
                    destination = None
                if destination in symbols:
                    annotation = f" <{symbols[destination]}>"
            print(
                f"{instruction.address:08x}: {instruction.mnemonic:8s} "
                f"{instruction.op_str}{annotation}"
            )


if __name__ == "__main__":
    main()
