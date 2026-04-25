"""Edge TTS wrapper that accepts SSML input for natural audiobook narration."""
import asyncio
import sys
import edge_tts


async def synthesize(voice: str, ssml_file: str, output_file: str):
    with open(ssml_file, "r") as f:
        ssml = f.read()
    communicate = edge_tts.Communicate(ssml, voice)
    await communicate.save(output_file)


if __name__ == "__main__":
    if len(sys.argv) != 4:
        print(f"Usage: {sys.argv[0]} <voice> <ssml_file> <output.mp3>", file=sys.stderr)
        sys.exit(1)
    asyncio.run(synthesize(sys.argv[1], sys.argv[2], sys.argv[3]))
