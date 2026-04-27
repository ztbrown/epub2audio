"""Edge TTS wrapper with prosody settings for natural audiobook narration."""
import asyncio
import sys
import edge_tts


async def synthesize(voice: str, text_file: str, output_file: str):
    with open(text_file, "r") as f:
        text = f.read()
    communicate = edge_tts.Communicate(text, voice, rate="-5%", pitch="-2Hz")
    await communicate.save(output_file)


if __name__ == "__main__":
    if len(sys.argv) != 4:
        print(f"Usage: {sys.argv[0]} <voice> <text_file> <output.mp3>", file=sys.stderr)
        sys.exit(1)
    asyncio.run(synthesize(sys.argv[1], sys.argv[2], sys.argv[3]))
