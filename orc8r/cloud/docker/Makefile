build:
	./build.py
run:
	./build.py && ./run.py
dev:
	docker-compose down ; ./build.py --all --parallel && ./run.py ; docker-compose logs -f controller
