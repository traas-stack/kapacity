FROM continuumio/miniconda3:23.5.2-0

WORKDIR /algorithm

ENV PYTHONPATH=/algorithm

COPY environment.yml .
RUN conda env create -f environment.yml

COPY kapacity/ kapacity/

ENTRYPOINT ["conda", "run", "--no-capture-output", "-n", "kapacity", \
            "python", "/algorithm/kapacity/portrait/horizontal/predictive/main.py"]
