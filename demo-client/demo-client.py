#!/usr/bin/env python3

import sys
import threading

from bs4 import BeautifulSoup
import requests


BASE_URL = 'http://localhost:8080'


class Client(threading.Thread):
    def __init__(self, id):
        super().__init__(group=None)
        self.id = id
        self.fp = open(f"./logs/log-{id+1}.log", 'w')
        self.session = requests.Session()
        
    def register(self):
        try:
            r = self.session.get(BASE_URL + '/register')
            if r.status_code != 200:
                sys.exit('GET /register failed')
            doc = BeautifulSoup(r.text, 'html.parser')
            token = doc.find(attrs={'name': 'token'})['value']
            data = { 'first_name': 'Demo',
                     'last_name': f"Mc{self.id}",
                     'email': f"demo{self.id}@example.com",
                     'password': 'secret',
                     'password_verify': 'secret',
                     'token': token }
            r = self.session.post(BASE_URL + '/register', data=data)
            print('Register:', self.id)
        except Exception as exc:
            print(exc)
            sys.exit('Exception: failed in register')

    def login(self):
        try:
            r = self.session.get(BASE_URL + '/login')
            if r.status_code != 200:
                sys.exit('GET /login failed')
            doc = BeautifulSoup(r.text, 'html.parser')
            token = doc.find(attrs={'name': 'token'})['value']
            data = { 'email': f"demo{self.id}@example.com",
                     'password': 'secret',
                     'token': token }
            r = self.session.post(BASE_URL + '/login', data=data)
            print('Login:', self.id)
        except Exception as exc:
            print(exc)
            sys.exit('Exception: failed in login')

    def run_stream(self):
        r = self.session.get(BASE_URL + '/events', stream=True)
        for line in r.iter_lines():
            dline = line.decode('utf-8')
            print(dline, file=self.fp)
            self.fp.flush()

    def run(self):
        print('Running as ID', self.id)
        self.register()
        self.login()
        self.run_stream()

def run():
    if len(sys.argv) != 2:
        sys.exit('Usage: demo-client <nclients>')
    nclients = int(sys.argv[1])
    print('Running with', id, 'clients')

    clients = []
    for i in range(nclients):
        c = Client(i)
        clients.append(c)
        c.start()
    
if __name__ == '__main__':
    run()
